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
	const earlyCloneThresh2 = 0.82
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
		cnt, _ := m.ApplyPreview(b, player) // 只算感染数，快
		order[i] = scored{m, cnt}
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
			nb := cloneBoardPool(b)
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

	// 默认选最优手
	choice := bestMoves[0]

	// 当存在多手同分，且差距 < ε（这里用 2 分作阈值）时，随机挑一手
	if len(bestMoves) > 1 && bestScore-secondScore < 2 {
		choice = bestMoves[rand.Intn(len(bestMoves))]
	}

	return choice, true

}

// ------------------------------------------------------------
// α-β + 置换表
// ------------------------------------------------------------
func alphaBeta(
	b *Board,
	hash uint64,
	current, original CellState,
	depth, alpha, beta int,
) int {
	// 生成所有走子
	moves := GenerateMoves(b, current)
	// 1) 叶节点或无走子：写表后直接返回
	if depth == 0 || len(moves) == 0 {
		val := evaluate(b, original)
		storeTT(hash, depth, val, ttExact)
		return val
	}

	// 2) 置换表探测
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

	// 3) PV-Move 排序：注意这里先把 ok、idx 顺序写对
	if ok, idx := probeBestIdx(hash); ok {
		i := int(idx)
		if i < len(moves) {
			moves[0], moves[i] = moves[i], moves[0]
		}
	}

	var best int
	var bestIdx uint8

	if current == original {
		// 极大化节点
		best = math.MinInt32
		for i, m := range moves {
			nb := acquireBoard(b.radius)
			// 手动复制 cells
			for coord, st := range b.cells {
				nb.cells[coord] = st
			}
			// 增量哈希：从父哈希 xor 掉走子前后
			childHash := hash ^
				zobristKey(m.From, current) ^
				zobristKey(m.To, current)

			_, _ = m.Apply(nb, current)
			score := alphaBeta(nb, childHash,
				Opponent(current), original,
				depth-1, alpha, beta)
			releaseBoard(nb)

			if score > best {
				best = score
				bestIdx = uint8(i)
			}
			if score > alpha {
				alpha = score
			}
			if alpha >= beta {
				break
			}
		}
	} else {
		// 极小化节点
		best = math.MaxInt32
		for i, m := range moves {
			nb := acquireBoard(b.radius)
			for coord, st := range b.cells {
				nb.cells[coord] = st
			}
			childHash := hash ^
				zobristKey(m.From, current) ^
				zobristKey(m.To, current)

			_, _ = m.Apply(nb, current)
			score := alphaBeta(nb, childHash,
				Opponent(current), original,
				depth-1, alpha, beta)
			releaseBoard(nb)

			if score < best {
				best = score
				bestIdx = uint8(i)
			}
			if score < beta {
				beta = score
			}
			if beta <= alpha {
				break
			}
		}
	}

	// 4) 写回置换表
	var flag ttFlag
	switch {
	case best <= alphaOrig:
		flag = ttUpper
	case best >= betaOrig:
		flag = ttLower
	default:
		flag = ttExact
	}
	storeTT(hash, depth, best, flag)
	storeBestIdx(hash, bestIdx)
	return best
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
