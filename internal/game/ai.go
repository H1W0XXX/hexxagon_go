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

// const useLearned = false
const useLearned = false
const jumpMovePenalty = 25

// ------------------------------------------------------------
// 公共入口
// ------------------------------------------------------------
func cloneBoardPool(b *Board) *Board {
	nb := acquireBoard(b.radius)
	// —— 确保 cells map 已初始化且已清空 ——
	if nb.cells == nil {
		nb.cells = make(map[HexCoord]CellState, len(b.cells))
	} else {
		for k := range nb.cells {
			delete(nb.cells, k)
		}
	}
	// 复制 cells 数据
	for c, s := range b.cells {
		nb.cells[c] = s
	}
	// 同步 hash
	nb.hash = b.hash
	return nb
}

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

	if mv, ok := findImmediateWinOrSafeClone(b, player); ok {
		return mv, true
	}
	moves := GenerateMoves(b, player)
	if len(moves) == 0 {
		return Move{}, false
	}

	// —— 新增：根节点就过滤 0 感染跳越 ——
	moves = filterZeroInfectJumpsOrFallback(b, player, moves)
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
		// 记录哈希
		origHash := b.hash
		// 执行落子
		undo := mMakeMoveWithUndo(b, m, player)
		// 静态评估
		var score int
		if useLearned {
			score = Predict(b, player)
		} else {
			score = evaluateStatic(b, player)
		}

		// 回溯
		b.UnmakeMove(undo)
		b.hash = origHash

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
			// 【改动】从池里拿，不要用 cloneBoard
			nb := cloneBoardPool(b)
			//_, _ = it.mv.MakeMove(nb, player)
			// 清理一下，避免池里残留
			nb.LastMove = Move{}
			// 用统一入口，保证 LastMove 被写入
			_ = mMakeMoveWithUndo(nb, it.mv, player) // 丢掉 undo 没关系，这块本来就不回滚
			score := alphaBeta(
				nb, nb.hash,
				Opponent(player), player,
				depth-1, alphaRoot, betaRoot,
			)
			// 用完再放回池里
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

	// 当存在多手同分，且差距 < ε（这里用 3 分作阈值）时，随机挑一手
	if len(bestMoves) > 1 && bestScore-secondScore < 3 {
		choice = bestMoves[rand.Intn(len(bestMoves))]
	}

	return choice, true

}

// ------------------------------------------------------------
// α-β + 置换表
// ------------------------------------------------------------
func mMakeMoveWithUndo(b *Board, mv Move, player CellState) undoInfo {
	// 确保评估能看到“刚才这步”
	b.LastMove = mv

	infected, u := mv.MakeMove(b, player)
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
	//r := float64(empties) / float64(len(coords))
	// ————————————————————————————————————————————————————————————————

	// 1) 生成所有走法
	moves := GenerateMoves(b, current)

	// 2) 叶节点或无走子：直接评估，并写入置换表
	if depth == 0 || len(moves) == 0 {
		var val int
		if useLearned {
			val = Predict(b, original)
		} else {
			val = evaluateStatic(b, original)
		}
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

		// ———— 新增：过滤开局阶段 0 感染跳跃 ————
		var filtered []Move
		for _, mv := range moves {
			//if r >= openingPhaseThresh && mv.IsJump() && previewInfectedCount(b, mv, current) == 0 {
			if mv.IsJump() && previewInfectedCount(b, mv, current) == 0 {
				// 跳跃但0感染，丢弃
				continue
			}
			filtered = append(filtered, mv)
		}
		moves = filtered
		// 如果全部走法都被过滤，回退到原始走法列表，避免空搜索
		//if len(moves) == 0 {
		//	moves = GenerateMoves(b, current)
		//}
		// ——————————————————————————————

		// PV-Move 排序：置换表里记录的最佳索引先尝试
		if ok, idx := probeBestIdx(hash); ok {
			i := int(idx)
			if i < len(moves) {
				moves[0], moves[i] = moves[i], moves[0]
			}
		}

		// 遍历剩余走法
		for i, mv := range moves {
			// 计算增量 Zobrist hash
			origHash := b.hash
			childHash := origHash ^ zobristSide[sideIdx(current)]

			// from → Empty（若跳跃则额外清除原位）
			childHash ^= zobristKey(mv.From, current)
			if mv.IsJump() {
				childHash ^= zobristKey(mv.From, Empty)
			}
			// to → current
			childHash ^= zobristKey(mv.To, Empty)
			childHash ^= zobristKey(mv.To, current)
			// 感染翻转
			for _, n := range b.Neighbors(mv.To) {
				if b.Get(n) == Opponent(current) {
					childHash ^= zobristKey(n, Opponent(current))
					childHash ^= zobristKey(n, current)
				}
			}
			// 切换行棋方
			next := Opponent(current)
			childHash ^= zobristSide[sideIdx(next)]
			// 更新棋盘 hash
			b.hash = childHash

			// 真正落子
			undo := mMakeMoveWithUndo(b, mv, current)

			// 递归搜索
			score := alphaBeta(b, childHash, next, original, depth-1, alpha, beta)

			// 回溯
			b.UnmakeMove(undo)
			b.hash = origHash

			// （可选）对所有跳跃加上固定惩罚，比如 jumpMovePenalty
			if mv.IsJump() && !useLearned {
				score -= jumpMovePenalty
			}

			// 更新 bestScore / α / β-剪枝
			if score > bestScore {
				bestScore = score
				bestIdx = uint8(i)
			}
			if score > alpha {
				alpha = score
				if alpha >= beta {
					break
				}
			}
		}
	} else {
		// === MIN 节点 ===
		bestScore = math.MaxInt32
		for i, mv := range moves {
			// 如果你只想给 MAX 侧惩罚，那么这里可以不做任何改动；否则下面也可以照着 MAX 的做法—给 MIN 侧的“非感染跳跃”一个很高的分数，使 MIN 不愿意选它。
			// 通常我们只对 MAX 侧进行“非感染跳跃惩罚”，所以这里不加惩罚判断——保持原样即可。

			childHash := hash
			// ★ 0) 去掉当前行棋方
			childHash ^= zobristSide[sideIdx(current)]

			// ①–⑤ 增量 XOR 格子状态
			childHash ^= zobristKey(mv.From, current)

			//if mv.IsJump() {
			//	childHash = childHash ^ zobristKey(mv.From, current) ^ zobristKey(mv.From, Empty)
			//}
			next := Opponent(current)
			childHash ^= zobristSide[sideIdx(next)] // ★ 新增

			childHash = childHash ^ zobristKey(mv.To, Empty) ^ zobristKey(mv.To, current)

			// 执行落子并记录 undo
			undo := mMakeMoveWithUndo(b, mv, current)

			// 递归
			score := alphaBeta(b, childHash, Opponent(current), original, depth-1, alpha, beta)

			// 回溯
			b.UnmakeMove(undo)

			if mv.IsJump() {
				if !useLearned {
					// 由于 MIN 节点是在找最小 score，所以想让它不喜欢跳，就给它加一个很大的正分：
					score += jumpMovePenalty
				}

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

func chooseEndgameDepth(b *Board, base int) int {
	// 统计空格
	empties := 0
	for _, c := range b.AllCoords() {
		if b.Get(c) == Empty {
			empties++
		}
	}
	switch {
	case empties <= 6:
		// 残局很小，基本可以搜到底（每回合至少占/改变1格，给点冗余）
		return base + 2
	case empties <= 10:
		return base + 2
	//case empties <= 14:
	//	return base + 2
	default:
		return base
	}
}
func findImmediateWinOrSafeClone(b *Board, p CellState) (Move, bool) {
	op := Opponent(p)
	best := Move{}
	found := false

	for _, mv := range GenerateMoves(b, p) {
		// 只考虑克隆（你就想防“跳了被反超”）
		if !mv.IsClone() {
			continue
		}

		nb := cloneBoard(b)
		nb.LastMove = mv
		_, _ = mv.MakeMove(nb, p)

		// 1) 立即终局 = 对手无棋 or 无空格：直接选
		empties := 0
		for _, c := range nb.AllCoords() {
			if nb.Get(c) == Empty {
				empties++
			}
		}
		if len(GenerateMoves(nb, op)) == 0 || empties == 0 {
			return mv, true
		}

		// 2) “一手后仍领先”的保胜：我方子数差 > 对手下一手最大感染
		my := nb.CountPieces(p)
		his := nb.CountPieces(op)
		diff := my - his
		// 估计对手下一手最多能吃多少（即时感染最大值）
		opMaxEat := 0
		for _, omv := range GenerateMoves(nb, op) {
			eat := previewInfectedCount(nb, omv, op)
			if eat > opMaxEat {
				opMaxEat = eat
			}
		}
		if diff > opMaxEat {
			best = mv
			found = true
			// 不 return：继续找有没有“更好”的（可直接 return 也行）
		}
	}
	return best, found
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
		depth2 := chooseEndgameDepth(root, depth)
		mv, hit := FindBestMoveAtDepth(root, player, depth2)
		if !hit {
			break // 无合法走法
		}
		// 记录本层 PV-Move：根节点 hash → idx=0
		pvMove[root.hash] = 0
		best, bestScore, ok = mv, 0, true // 根节点时 score 在内部已比较
	}
	return
}

func AlphaBeta(b *Board, player CellState, depth int) int {
	// 1) 把“行棋方”也异或进哈希，确保置换表区分 Max/Min
	initialHash := b.hash ^ zobristSide[sideIdx(player)]

	// 2) 调用内层实现：先轮到对手走，再到 player
	return alphaBeta(
		b,
		initialHash,
		Opponent(player), // current = 对手
		player,           // original = 我方
		depth,
		math.MinInt, // 初始 α
		math.MaxInt, // 初始 β
	)
}

// alphaBetaNoTT 在 b 上执行一次不带置换表的 α–β 搜索。
// - current: 当前行棋方
// - original: 根节点的行棋方，用于 evaluate 判断
// - depth: 剩余深度
// - alpha, beta: 剪枝界限
// ------------------------------------------------------------
// 对外包装器 —— 只要 3 个参数即可调用
// ------------------------------------------------------------
func AlphaBetaNoTT(b *Board, player CellState, depth int) int {
	// 根节点先把“行棋方随机键” XOR 进去，保证 hash 正确
	initialHash := hashBoard(b) ^ zobristSide[sideIdx(player)]
	b.hash = initialHash

	// 递归从对手开始（current），original = player
	return alphaBetaNoTT(
		b,
		Opponent(player), // current
		player,           // original
		depth,
		math.MinInt32,
		math.MaxInt32,
	)
}

// ------------------------------------------------------------
// 内部递归实现 —— 不暴露、多参数
// ------------------------------------------------------------
func alphaBetaNoTT(
	b *Board,
	current, original CellState,
	depth, alpha, beta int,
) int {
	// 递归终止：深度到 0 或无空位
	if depth == 0 || b.CountPieces(PlayerA)+b.CountPieces(PlayerB) == len(b.AllCoords()) {
		return evaluateStatic(b, original)
	}

	moves := GenerateMoves(b, current)

	if current == original {
		// -------- MAX 节点 --------
		best := math.MinInt32
		for _, mv := range moves {
			undo := mMakeMoveWithUndo(b, mv, current)
			score := alphaBetaNoTT(b, Opponent(current), original, depth-1, alpha, beta)
			b.UnmakeMove(undo)

			if score > best {
				best = score
			}
			if score > alpha {
				alpha = score
			}
			if alpha >= beta {
				break
			}
		}
		return best
	}

	// -------- MIN 节点 --------
	best := math.MaxInt32
	for _, mv := range moves {
		undo := mMakeMoveWithUndo(b, mv, current)
		score := alphaBetaNoTT(b, Opponent(current), original, depth-1, alpha, beta)
		b.UnmakeMove(undo)

		if score < best {
			best = score
		}
		if score < beta {
			beta = score
		}
		if beta <= alpha {
			break
		}
	}
	return best
}

// 根节点/任意节点可复用的过滤器：尽量剔除“0 感染跳跃”，但保证不至于空集合
func filterZeroInfectJumpsOrFallback(b *Board, side CellState, moves []Move) []Move {
	filtered := make([]Move, 0, len(moves))
	for _, mv := range moves {
		if mv.IsJump() && previewInfectedCount(b, mv, side) == 0 {
			continue
		}
		filtered = append(filtered, mv)
	}
	if len(filtered) > 0 {
		return filtered
	}
	// 如果全被剔空了，至少保留克隆；再不行就原样返回，避免无解
	clones := make([]Move, 0, len(moves))
	for _, mv := range moves {
		if mv.IsClone() {
			clones = append(clones, mv)
		}
	}
	if len(clones) > 0 {
		return clones
	}
	return moves
}
