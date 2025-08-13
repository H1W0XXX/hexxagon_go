// file: internal/game/evaluate.go
package game

// 可调参数
var (
	cloneThresh = 0.25      // 克隆/跳跃阈值
	jumpThresh  = 1.0 / 3.0 // 跳跃/残局阈值
	dangerW     = 40        // 暴露惩罚权重
)

// 加分上下限
const (
	CLONE_BONUS_MAX       = 30
	CLONE_BONUS_MIN       = 14
	JUMP_BONUS_MAX        = 3
	JUMP_BONUS_MIN        = 0
	blockW                = 2
	zeroInfectJumpPenalty = -5000
)

// ========== 新增部分：加权感染数与开局惩罚常量 ==========
const (
	// 开局阶段阈值：当空位比例 r ≥ 0.82 时，视为“开局”
	openingPhaseThresh = 0.82
	// 开局被对手感染时的额外惩罚权重
	openingPenaltyWeight = 10

	// 跳跃感染权重（跳跃感染得分较低）
	jumpInfWeight = 1
	// 克隆感染权重（克隆感染得分较高）
	cloneInfWeight = 2

	// ========= 新增 =========
	// 如果处于开局阶段(r ≥ openingPhaseThresh)，且我方存在可用克隆却没有使用克隆，
	// 那么静态评估扣分（值可以根据调试再微调）
	earlyJumpPenalty = -50
)

const (
	// 中期阶段阈值：当空位比例 r < 0.5 时视为“中期”
	midgamePhaseThresh = 0.6
	// 中期阶段，对手上一步周边位置权重
	midgameLastMoveWeight = 15
)

// HexCoord.Add：方便邻格计算
func (h HexCoord) Add(o HexCoord) HexCoord {
	return HexCoord{h.Q + o.Q, h.R + o.R}
}

// dynamicPieceW：原动态克隆权重逻辑
func dynamicPieceW(r float64) int {
	if r >= cloneThresh {
		t := (r - cloneThresh) / (1.0 - cloneThresh)
		return int(t*float64(CLONE_BONUS_MAX-CLONE_BONUS_MIN) + float64(CLONE_BONUS_MIN) + 0.5)
	}
	return CLONE_BONUS_MIN
}

// dynamicJumpW：原动态跳跃权重逻辑
func dynamicJumpW(r float64) int {
	if r <= jumpThresh {
		return JUMP_BONUS_MAX
	} else if r < 1.0 {
		t := (1.0 - r) / (1.0 - jumpThresh)
		return int(t*float64(JUMP_BONUS_MAX-JUMP_BONUS_MIN) + float64(JUMP_BONUS_MIN) + 0.5)
	}
	return JUMP_BONUS_MIN
}

// dynamicInfW：随空位比例 r，让感染分前期小、后期大
func dynamicInfW(r float64) int {
	const minW, maxW = 1, 4
	t := 1.0 - r
	return int(t*float64(maxW-minW) + float64(minW) + 0.5)
}

// maxInf：计算某方最大单步感染数
func maxInf(b *Board, pl CellState) int {
	best := 0
	for _, m := range GenerateMoves(b, pl) {
		if cnt, _ := m.ApplyPreview(b, pl); cnt > best {
			best = cnt
		}
	}
	return best
}

// isInOpponentRange：判断 c 是否在对手一步可达范围内
func isInOpponentRange(b *Board, c HexCoord, opponent CellState) bool {
	for _, dir := range Directions {
		nb := c.Add(dir)
		if b.Get(nb) == opponent {
			return true
		}
		for _, dir2 := range Directions {
			nb2 := nb.Add(dir2)
			if HexDist(nb2, c) == 2 && b.Get(nb2) == opponent {
				return true
			}
		}
	}
	return false
}

// ApplyPreview：在不修改棋盘的情况下预览感染数
func (m Move) ApplyPreview(b *Board, player CellState) (infected int, ok bool) {
	coords, undo := m.MakeMove(b, player)
	b.UnmakeMove(undo)
	return len(coords), true
}

// 对外导出
func Evaluate(b *Board, player CellState) int {
	return evaluateStatic(b, player)
}

func dynamicEdgeW(r float64) int {
	// r = 空格占比，开局 r≈1，残局 r≈0
	if r > 0.6 {
		return 6 // 开局大力冲边
	} else if r > 0.3 {
		return 3
	}
	return 1 // 残局趋于中性
}

func isOuter(c HexCoord, radius int) bool {
	ring := max3(abs(c.Q), abs(c.R), abs(c.Q+c.R))
	return ring == radius // 最外一圈
}

func outerRingCoords(b *Board) []HexCoord {
	var ring []HexCoord
	for _, c := range b.AllCoords() {
		if isOuter(c, b.radius) && b.Get(c) != Blocked {
			ring = append(ring, c)
		}
	}
	return ring
}

func compSizeAt(b *Board, start HexCoord, color CellState) int {
	if b.Get(start) != color {
		return 0
	}
	visited := make(map[HexCoord]bool, 16)
	stack := []HexCoord{start}
	visited[start] = true
	size := 0
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		size++
		for _, d := range cloneDirs {
			nb := HexCoord{cur.Q + d.Q, cur.R + d.R}
			if !visited[nb] && b.Get(nb) == color {
				visited[nb] = true
				stack = append(stack, nb)
			}
		}
	}
	return size
}

func evaluateStatic(b *Board, player CellState) int {
	op := Opponent(player)

	// —— 可调权重 —— //
	const (
		pieceW          = 5  // 1) 敌我棋子差
		edgeW           = 2  // 2) 我方外圈每子少量加分
		blockDiffW      = 4  // 3) 3+ 连通块数量差（<50% 填充才生效）
		cloneInfW       = 3  // 5) 克隆感染权重（> 跳越）
		jumpInfW        = 2  // 5) 跳越感染权重
		weakJumpPenalty = 50 // 4) 弱跳越重罚（可调 120~200）
	)

	// —— 基础统计 —— //
	coords := b.AllCoords()
	total := len(coords)
	empties := 0
	myCnt, opCnt := 0, 0
	for _, c := range coords {
		switch b.Get(c) {
		case Empty:
			empties++
		case player:
			myCnt++
		case op:
			opCnt++
		}
	}
	filledRatio := float64(total-empties) / float64(total)

	// 1) 敌我棋子差
	pieceScore := (myCnt - opCnt) * pieceW

	// 2) 我方外圈少量加分
	myEdge := 0
	for _, c := range coords {
		if b.Get(c) == player && isOuter(c, b.radius) {
			myEdge++
		}
	}
	edgeScore := myEdge * edgeW

	// 3) 3+ 连通块数量差（<50% 才生效）
	blockScore := 0
	if filledRatio < 0.4 {
		myBlocks := countBlocks(b, player)
		opBlocks := countBlocks(b, op)
		blockScore = (myBlocks - opBlocks) * blockDiffW
	}

	// 4) 弱跳越重罚（按你的新规则）
	weakJumpScore := 0
	if b.LastMove.IsJump() {
		mover := b.Get(b.LastMove.To) // 刚跳的那一方，跳后 To 颜色就是 mover
		if mover == PlayerA || mover == PlayerB {
			sameAdj := 0
			for _, d := range cloneDirs { // 6 邻
				nb := HexCoord{b.LastMove.To.Q + d.Q, b.LastMove.To.R + d.R}
				if b.Get(nb) == mover {
					sameAdj++
				}
			}
			//fmt.Printf("LM=%v  isJump=%v  sameAdj=%d  mover=%v\n",
			//	b.LastMove, b.LastMove.IsJump(), sameAdj, mover)

			if sameAdj <= 1 {
				if mover == player {
					weakJumpScore -= weakJumpPenalty
				} else {
					weakJumpScore += weakJumpPenalty
				}
			}
		}
	}

	// 5) 感染潜力（克隆加分 > 跳越）
	//    用“下一手最大即刻感染数”的差来刻画潜力，并对克隆/跳跃分别加权
	maxCloneJump := func(side CellState) (cloneMax, jumpMax int) {
		for _, m := range GenerateMoves(b, side) {
			cnt := previewInfectedCount(b, m, side)
			if m.IsClone() {
				if cnt > cloneMax {
					cloneMax = cnt
				}
			} else { // Jump
				if cnt > jumpMax {
					jumpMax = cnt
				}
			}
		}
		return
	}
	myCloneMax, myJumpMax := maxCloneJump(player)
	opCloneMax, opJumpMax := maxCloneJump(op)
	infPotentialScore := (myCloneMax*cloneInfW + myJumpMax*jumpInfW) -
		(opCloneMax*cloneInfW + opJumpMax*jumpInfW)

	// —— 汇总 —— //
	final := pieceScore +
		edgeScore +
		blockScore +
		weakJumpScore +
		infPotentialScore

	return final
}

// ─────────────────────────────────────────────────────────────────────────────
// 辅助：统计“跳跃”走法能够感染的最大棋子数
// ─────────────────────────────────────────────────────────────────────────────
func maxJumpInfFromMoves(b *Board, player CellState, moves []Move) int {
	op := Opponent(player)
	maxInf := 0

	for _, m := range moves {
		if !m.IsJump() {
			continue
		}

		// 估算：跳到 m.To 后，m.To 周围 6 个方向属于对手的棋子都会被感染
		cnt := 0
		for _, dir := range Directions {
			nb := m.To.Add(dir)
			if b.Get(nb) == op {
				cnt++
			}
		}
		if cnt > maxInf {
			maxInf = cnt
		}
	}
	return maxInf
}

func maxInfFromMoves(b *Board, pl CellState, moves []Move) int {
	best := 0
	for _, m := range moves {
		cnt := previewInfectedCount(b, m, pl)
		if cnt > best {
			best = cnt
		}
	}
	return best
}

// 统计连通块数：每个连通块只要 size>=3 就计 +1
func countBlocks(b *Board, player CellState) int {
	visited := make(map[HexCoord]bool)
	blocks := 0
	for _, c := range b.AllCoords() {
		if visited[c] || b.Get(c) != player {
			continue
		}
		// flood-fill 收集这个连通块
		size := 0
		stack := []HexCoord{c}
		visited[c] = true
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			for _, nb := range b.Neighbors(cur) {
				if !visited[nb] && b.Get(nb) == player {
					visited[nb] = true
					stack = append(stack, nb)
				}
			}
		}
		// 只要连通块大小 ≥3，就 +1
		if size >= 3 {
			blocks++
		}
	}
	return blocks
}

// evaluateHoles 评估空洞区域被对手跳入的惩罚
func evaluateHoles(b *Board, player CellState) int {
	op := Opponent(player)
	// 记录已访问空格
	visited := make(map[HexCoord]bool)
	holePenalty := 0
	holeWeight := 5 // 空洞区域被对手跳入即惩罚，可调

	// 遍历所有空格，找到未访问的空洞区域
	for _, start := range b.AllCoords() {
		if b.Get(start) != Empty || visited[start] {
			continue
		}
		// BFS 收集连通空洞
		queue := []HexCoord{start}
		region := []HexCoord{start}
		visited[start] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, nb := range b.Neighbors(cur) {
				if !visited[nb] && b.Get(nb) == Empty {
					visited[nb] = true
					queue = append(queue, nb)
					region = append(region, nb)
				}
			}
		}

		// 收集对手棋子位置
		oppPositions := make([]HexCoord, 0)
		for _, c := range b.AllCoords() {
			if b.Get(c) == op {
				oppPositions = append(oppPositions, c)
			}
		}

		// 如果对手能在1或2步内进入该区域，则惩罚此区域大小
		opponentCanReach := false
		for _, cell := range region {
			if opponentCanReach {
				break
			}
			for _, opp := range oppPositions {
				if HexDist(opp, cell) <= 2 {
					opponentCanReach = true
					break
				}
			}
		}
		if opponentCanReach {
			holePenalty += len(region) * holeWeight
		}
	}
	return holePenalty
}

// maxWeightedInfFromMoves：在不修改棋盘情况下，计算“加权”后的最大感染数
func maxWeightedInfFromMoves(b *Board, pl CellState, moves []Move) int {
	best := 0
	for _, m := range moves {
		// 直接数邻居感染数，无需克隆棋盘
		cnt := previewInfectedCount(b, m, pl)

		// 跳跃感染权重为 jumpInfWeight，克隆感染乘以 cloneInfWeight
		if m.IsJump() {
			cnt = cnt * jumpInfWeight
		} else {
			cnt = cnt * cloneInfWeight
		}
		if cnt > best {
			best = cnt
		}
	}
	return best
}

// “预览”一次感染数，而不实际修改棋盘
func previewInfectedCount(b *Board, mv Move, player CellState) int {
	count := 0
	for _, dir := range Directions {
		nb := mv.To.Add(dir)
		if b.Get(nb) == Opponent(player) {
			count++
		}
	}
	return count
}

// Predict 改为调用 CNN 的 value，失败则回退到静态评估
//func Predict(b *Board, player CellState) int {
//	if _, v, err := CNNPredict(b, player); err == nil {
//		// value ∈ (-1,1) → 映射到分数区间（可按你原有量级调）
//		fmt.Printf("模型错误 %v \n", err)
//		return int(math.Round(float64(v) * 100.0))
//	}
//	// 回退：原 evaluateStatic
//	return evaluateStatic(b, player)
//}
