// game/ai.go
package game

import (
	//"fmt"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
	runtime.GOMAXPROCS(runtime.NumCPU() - 1) // 吃满物理/逻辑核心

}

// ------------------------------------------------------------
// 公共入口
// ------------------------------------------------------------
func cloneBoardPool(b *Board) *Board {
	nb := acquireBoard(b.radius)
	// 复制 cells
	for c, s := range b.cells {
		nb.cells[c] = s
	}
	nb.hash = b.hash
	return nb
}

const jumpMovePenalty = 25

func cloneBoard(b *Board) *Board {
	// 分配全新的 map，绝不复用
	nb := &Board{
		radius: b.radius,
		cells:  make(map[HexCoord]CellState, len(b.cells)),
		hash:   b.hash,
	}
	for c, s := range b.cells {
		nb.cells[c] = s
	}
	return nb
}

func FindBestMoveAtDepth(b *Board, player CellState, depth int) (Move, bool) {
	ttProbeCount = 0
	ttHitCount = 0

	moves := GenerateMoves(b, player)
	if len(moves) == 0 {
		return Move{}, false
	}

	//const depth = 4
	const inf = 1 << 30

	// 1) 计算空位比例 r
	coords := b.AllCoords()
	empties := 0
	for _, c := range coords {
		if b.Get(c) == Empty {
			empties++
		}
	}
	r := float64(empties) / float64(len(coords))
	// --- 开局极早期强制只克隆 ---
	const earlyCloneThresh = 0.76 // 当空位 ≥90%，视为开局极早期
	const earlyCloneThresh2 = 0.76
	//fmt.Println(r)
	if r >= earlyCloneThresh2 {
		// 开局早期：只保留“外圈克隆”走法
		var edgeClones []Move
		for _, m := range moves {
			if m.IsClone() && isOuter(m.To, b.radius) {
				edgeClones = append(edgeClones, m)
			}
		}
		// 如果确实有外圈克隆，就用它们；否则保留所有克隆
		if len(edgeClones) > 0 {
			moves = edgeClones
		} else {
			var clones []Move
			for _, m := range moves {
				if m.IsClone() {
					clones = append(clones, m)
				}
			}
			moves = clones
		}
	}

	// ---------- 1) 走法粗评分（真实 evaluate） ----------
	type scored struct {
		mv    Move
		score int
	}
	order := make([]scored, len(moves))

	for i, m := range moves {
		nb := cloneBoard(b) // 手工新分配
		m.MakeMove(nb, player)
		score := evaluateStatic(nb, player)
		// 没有 releaseBoard(nb)
		order[i] = scored{m, score}
	}

	sort.Slice(order, func(i, j int) bool {
		if order[i].score != order[j].score {
			return order[i].score > order[j].score // 感染多的先
		}
		// 同感染数：Clone 先、Jump 后
		if order[i].mv.IsClone() != order[j].mv.IsClone() {
			return order[i].mv.IsClone() // Clone=true 放前面
		}
		return false
	})
	// ---------- 2) 并行根节点 α–β 搜索 ----------
	type result struct {
		mv    Move
		score int
	}
	resCh := make(chan result, len(order))
	var wg sync.WaitGroup

	alphaRoot, betaRoot := -inf, inf

	for _, item := range order {
		wg.Add(1)
		go func(it scored) {
			defer wg.Done()
			// 一样用对象池克隆
			nb := cloneBoard(b)
			_, _ = it.mv.MakeMove(nb, player)
			score := alphaBeta(nb, nb.hash,
				Opponent(player), player, depth-1, alphaRoot, betaRoot)
			releaseBoard(nb)

			resCh <- result{it.mv, score}
		}(item)
	}
	wg.Wait()
	close(resCh)
	probes, hits, rate := GetTTStats()
	_, _, _ = probes, hits, rate
	//fmt.Printf("TT probes: %d, hits: %d, hit rate: %.2f%%\n", probes, hits, rate)
	// ---------- 3) 汇总最佳 + ε–贪心同分支 ----------
	bestScore := -inf
	secondScore := -inf
	var bestMoves []Move

	for r := range resCh {
		score := r.score

		// 如果当前分数高于 bestScore，更新 bestScore 和 secondScore
		if score > bestScore {
			secondScore = bestScore
			bestScore = score
			bestMoves = []Move{r.mv}

			// 如果当前分数介于 secondScore 和 bestScore 之间，更新 secondScore
		} else if score > secondScore && score < bestScore {
			secondScore = score

			// 如果刚好等于 bestScore，就加入候选列表
		} else if score == bestScore {
			bestMoves = append(bestMoves, r.mv)
		}
	}

	var cloneMoves []Move
	for _, m := range bestMoves {
		if m.IsClone() {
			cloneMoves = append(cloneMoves, m)
		}
	}
	if len(cloneMoves) > 0 {
		bestMoves = cloneMoves
	}

	// 默认选最优手
	choice := bestMoves[0]

	// 当存在多手同分，且差距 < ε（这里用 1 分作阈值）时，随机挑一手
	if len(bestMoves) > 1 && bestScore-secondScore < 1 {
		choice = bestMoves[rand.Intn(len(bestMoves))]
	}

	return choice, true

}

// ------------------------------------------------------------
// α-β + 置换表
// ------------------------------------------------------------
func mMakeMoveWithUndo(b *Board, mv Move, player CellState) undoInfo {
	infected, u := mv.MakeMove(b, player)
	// infected 可以用于播放动画或分析，但对搜索本身不需要特别处理
	_ = infected
	return u
}
func alphaBeta(
	b *Board,
	hash uint64,
	current, original CellState,
	depth, alpha, beta int,
) int {
	// ———— 新增 —— 在函数开头，先计算空位比例 r，用于判断是否处于“开局前期” ————
	coords := b.AllCoords()
	empties := 0
	for _, c := range coords {
		if b.Get(c) == Empty {
			empties++
		}
	}
	r := float64(empties) / float64(len(coords))
	// ————————————————————————————————————————————————————————————————

	// 1) 生成所有走法
	moves := GenerateMoves(b, current)

	// 2) 叶节点或无走子：直接评估，并写入置换表
	if depth == 0 || len(moves) == 0 {
		val := evaluateStatic(b, original)
		storeTT(hash, depth, val, ttExact)
		return val
	}

	// 3) 置换表探测
	if hit, val, flag := probeTT(hash, depth); hit {
		switch flag {
		case ttExact:
			return val
		case ttLower:
			if val > alpha {
				alpha = val
			}
		case ttUpper:
			if val < beta {
				beta = val
			}
		}
		if alpha >= beta {
			return val
		}
	}
	alphaOrig := alpha
	betaOrig := beta

	// 4) PV-Move 排序：如果置换表里有记录的最佳走法索引，把它交换到 moves[0]
	if ok, idx := probeBestIdx(hash); ok {
		i := int(idx)
		if i < len(moves) {
			moves[0], moves[i] = moves[i], moves[0]
		}
	}

	var bestScore int
	var bestIdx uint8

	// 5) 根据是“极大化节点”还是“极小化节点”分别处理
	if current == original {
		// === MAX 节点 ===
		bestScore = math.MinInt32
		for i, mv := range moves {
			// ———— 新增 —— 对“开局前期非感染跳跃”进行惩罚 ——
			if r >= openingPhaseThresh && mv.IsJump() {
				infected := previewInfectedCount(b, mv, current)
				if infected == 0 {
					// 非感染跳跃，罚分
					score := math.MinInt32 / 2
					// 更新 bestScore / alpha
					if score > bestScore {
						bestScore = score
						bestIdx = uint8(i)
					}
					if score > alpha {
						alpha = score
					}
					continue
				}
			}
			// ——————————————————————————————————————————————

			// 先保存“父节点的 hash”
			origHash := b.hash

			// 计算 childHash：先把 from/to/感染全部 xor 掉、再 xor 进新状态
			childHash := origHash
			// ① 把 from(原来是 current) xor 掉
			childHash ^= zobristKey(mv.From, current)
			// ② 如果是 Jump，把 from→Empty
			if mv.IsJump() {
				childHash ^= zobristKey(mv.From, Empty)
			}
			// ③ 把 to(原来是 Empty) xor 掉
			childHash ^= zobristKey(mv.To, Empty)
			// ④ 把 to→ current
			childHash ^= zobristKey(mv.To, current)
			// ⑤ 感染格：foreach n in b.Neighbors(mv.To) { if b.Get(n)==Opponent(current) {
			//         childHash ^= zobristKey(n, Opponent(current));
			//         childHash ^= zobristKey(n, current);
			//     }
			// }
			for _, n := range b.Neighbors(mv.To) {
				if b.Get(n) == Opponent(current) {
					childHash ^= zobristKey(n, Opponent(current))
					childHash ^= zobristKey(n, current)
				}
			}

			// 让 b.hash 也同步修改到 childHash
			b.hash = childHash

			// 真正执行 MakeMove，并记录 undo
			undo := mMakeMoveWithUndo(b, mv, current)

			// 递归下去
			score := alphaBeta(b, childHash, Opponent(current), original, depth-1, alpha, beta)

			// 回溯：先把棋盘内容恢复
			b.UnmakeMove(undo)
			// 再把 b.hash 恢复
			b.hash = origHash

			if mv.IsJump() {
				score -= jumpMovePenalty
			}

			// 更新 bestScore / α / 剪枝
			if score > bestScore {
				bestScore = score
				bestIdx = uint8(i)
			}
			if score > alpha {
				alpha = score
			}
			if alpha >= beta {
				// 触发 β-剪枝，直接跳出
				break
			}
		}
	} else {
		// === MIN 节点 ===
		bestScore = math.MaxInt32
		for i, mv := range moves {
			// 如果你只想给 MAX 侧惩罚，那么这里可以不做任何改动；否则下面也可以照着 MAX 的做法—给 MIN 侧的“非感染跳跃”一个很高的分数，使 MIN 不愿意选它。
			// 通常我们只对 MAX 侧进行“非感染跳跃惩罚”，所以这里不加惩罚判断——保持原样即可。

			// 同样要增量更新哈希
			childHash := hash ^
				zobristKey(mv.From, current) ^
				zobristKey(mv.To, Empty)
			if mv.IsJump() {
				childHash = childHash ^ zobristKey(mv.From, current) ^ zobristKey(mv.From, Empty)
			}
			childHash = childHash ^ zobristKey(mv.To, Empty) ^ zobristKey(mv.To, current)

			// 执行落子并记录 undo
			undo := mMakeMoveWithUndo(b, mv, current)

			// 递归
			score := alphaBeta(b, childHash, Opponent(current), original, depth-1, alpha, beta)

			// 回溯
			b.UnmakeMove(undo)

			if mv.IsJump() {
				// 由于 MIN 节点是在找最小 score，所以想让它不喜欢跳，就给它加一个很大的正分：
				score += jumpMovePenalty
			}

			// 更新 best, β, 剪枝
			if score < bestScore {
				bestScore = score
				bestIdx = uint8(i)
			}
			if score < beta {
				beta = score
			}
			if beta <= alpha {
				// 触发 α-剪枝
				break
			}
		}
	}

	// 6) 写回置换表
	var flag ttFlag
	switch {
	case bestScore <= alphaOrig:
		flag = ttUpper
	case bestScore >= betaOrig:
		flag = ttLower
	default:
		flag = ttExact
	}
	storeTT(hash, depth, bestScore, flag)
	storeBestIdx(hash, bestIdx)
	return bestScore
}

// ------------------------------------------------------------
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func DeepSearch(b *Board, hash uint64, side CellState, depth int) int {

	return alphaBeta(b, hash, side, side, depth, -32000, 32000)
}

func IterativeDeepening(
	root *Board,
	player CellState,
	maxDepth int,
) (best Move, bestScore int, ok bool) {

	// 用于存上一层 PV 走法的哈希 → bestIdx
	pvMove := make(map[uint64]uint8)

	for depth := 1; depth <= maxDepth; depth++ {
		// 把上一层保存的 PV-Move 写进 TT，供排序
		for h, idx := range pvMove {
			storeBestIdx(h, idx)
		}
		// 调用已有的并行根节点搜索
		mv, hit := FindBestMoveAtDepth(root, player, depth)
		if !hit {
			break // 无合法走法
		}
		// 记录本层 PV-Move：根节点 hash → idx=0
		pvMove[root.hash] = 0
		best, bestScore, ok = mv, 0, true // 根节点时 score 在内部已比较
	}
	return
}
