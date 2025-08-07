// file: internal/game/evaluate.go
package game

import "math"

// 你从训练结果里抄过来的
var learnedW = []float64{
	-0.15342230,
	-0.05578179,
	-0.00084953,
	0.08653770,
	0.02525369,
	-0.00093363,
	0.00704528,
	-0.00498269,
	0.02515402,
	-0.00310400,
	0.09628337,
}

const learnedB = -1.8162169456481934

// 可调参数
var (
	cloneThresh = 0.25      // 克隆/跳跃阈值
	jumpThresh  = 1.0 / 3.0 // 跳跃/残局阈值
	dangerW     = 40        // 暴露惩罚权重
)

// 加分上下限
const (
	CLONE_BONUS_MAX = 30
	CLONE_BONUS_MIN = 14
	JUMP_BONUS_MAX  = 3
	JUMP_BONUS_MIN  = 0
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
	earlyJumpPenalty = 1
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

func evaluateStatic(b *Board, player CellState) int {
	op := Opponent(player)

	// 1) 计算空位比例 r
	coords := b.AllCoords()
	empties := 0
	for _, c := range coords {
		if b.Get(c) == Empty {
			empties++
		}
	}
	r := float64(empties) / float64(len(coords))

	// 2) 如果处于“开局前期”（r ≥ openingPhaseThresh），且对手子数多于我方，就严重惩罚
	myCnt, opCnt := 0, 0
	for _, c := range coords {
		switch b.Get(c) {
		case player:
			myCnt++
		case op:
			opCnt++
		}
	}
	openingPenalty := 0
	if r >= openingPhaseThresh && opCnt > myCnt {
		openingPenalty = (opCnt - myCnt) * openingPenaltyWeight
	}

	// 3) 动态权重
	edgeW := dynamicEdgeW(r)
	pieceW := dynamicPieceW(r)
	//jumpW := dynamicJumpW(r)
	infW := dynamicInfW(r)

	// 4) 统计边缘 / 危险
	outer, danger := 0, 0
	for _, c := range coords {
		s := b.Get(c)
		if s == Empty {
			continue
		}
		onEdge := isOuter(c, b.radius)
		if s == player {
			if onEdge {
				outer++
			}
			if isInOpponentRange(b, c, op) {
				danger++
			}
		} else { // 对手
			if onEdge {
				outer-- // 对手在边缘记负分
			}
		}
	}

	// 5) 生成走法
	myMoves := GenerateMoves(b, player)
	opMoves := GenerateMoves(b, op)

	// 克隆机动性差
	myCloneCount, opCloneCount := 0, 0
	for _, m := range myMoves {
		if m.IsClone() {
			myCloneCount++
		}
	}
	for _, m := range opMoves {
		if m.IsClone() {
			opCloneCount++
		}
	}
	//cloneMobDiff := myCloneCount - opCloneCount
	//fullMobDiff := len(myMoves) - len(opMoves)

	// 6) 加权感染差
	infDiffWeighted := maxWeightedInfFromMoves(b, player, myMoves) -
		maxWeightedInfFromMoves(b, op, opMoves)

	// 7) 早期跳跃惩罚（修改点）
	//
	//    若仍在开局阶段且存在可克隆走法，但“最佳跳跃”感染不足 2 颗棋子，
	//    则认为跳跃收益太低，施加惩罚；否则不扣分。
	maxJumpInf := maxJumpInfFromMoves(b, player, myMoves) // ← 新增
	earlyJumpCost := 0
	if r >= openingPhaseThresh && myCloneCount > 0 && maxJumpInf < 2 {
		earlyJumpCost = earlyJumpPenalty
	}

	// 8) 其他打分项
	outerScore := outer * edgeW
	holesPenalty := -evaluateHoles(b, player)
	pieceScore := (myCnt - opCnt) * pieceW
	safetyScore := -danger * dangerW

	//mobScore := 0
	//if r >= openingPhaseThresh {
	//	mobScore = cloneMobDiff * jumpW / 3
	//} else {
	//	mobScore = fullMobDiff * jumpW / 3
	//}

	finalScore :=
		pieceScore*2 +
			//mobScore +
			infDiffWeighted*infW/2 +
			outerScore +
			safetyScore +
			holesPenalty -
			openingPenalty -
			earlyJumpCost

	// —— 中期对手上一步周边加权 —— //
	if r < midgamePhaseThresh {
		for _, dir := range Directions {
			nb := b.LastMove.To.Add(dir)
			if b.Get(nb) == Empty {
				finalScore += midgameLastMoveWeight
			}
		}
	}
	return finalScore
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

// ExtractFeatures 从棋盘状态和当前玩家提取一组特征，用于训练或线性评估
func ExtractFeatures(b *Board, player CellState) []float64 {
	op := Opponent(player)
	coords := b.AllCoords()
	// 1) 空位比例 r
	empties := 0
	for _, c := range coords {
		if b.Get(c) == Empty {
			empties++
		}
	}
	n := float64(len(coords))
	r := float64(empties) / n

	// 2) 棋子数差
	myCnt, opCnt := 0, 0
	outer, danger := 0, 0
	for _, c := range coords {
		s := b.Get(c)
		if s == player {
			myCnt++
			// 边缘
			if isOuter(c, b.radius) {
				outer++
			}
			// 危险
			if isInOpponentRange(b, c, op) {
				danger++
			}
		} else if s == op {
			opCnt++
			if isOuter(c, b.radius) {
				outer--
			}
		}
	}

	// 3) 机动性差：克隆或跳跃计数
	myMoves := GenerateMoves(b, player)
	opMoves := GenerateMoves(b, op)
	cloneMobDiff, fullMobDiff := 0, len(myMoves)-len(opMoves)
	for _, m := range myMoves {
		if m.IsClone() {
			cloneMobDiff++
		}
	}

	mobDiff := fullMobDiff
	if r >= openingPhaseThresh {
		mobDiff = cloneMobDiff
	}

	// 4) 加权感染差
	infDiffWeighted := maxWeightedInfFromMoves(b, player, myMoves) -
		maxWeightedInfFromMoves(b, op, opMoves)

	// 5) 洞惩罚
	holesPenalty := -evaluateHoles(b, player)

	// 6) 开局惩罚
	openingPenalty := 0
	if r >= openingPhaseThresh && opCnt > myCnt {
		openingPenalty = (opCnt - myCnt) * openingPenaltyWeight
	}

	// 7) 早期跳跃惩罚
	earlyJumpCost := 0
	if r >= openingPhaseThresh && cloneMobDiff > 0 && infDiffWeighted == 0 {
		earlyJumpCost = earlyJumpPenalty
	}

	// 8) 中期对手上一步周边加权
	lastMoveBonus := 0
	if r < midgamePhaseThresh && b.LastMove.To != (HexCoord{}) {
		for _, dir := range Directions {
			nb := b.LastMove.To.Add(dir)
			if b.Get(nb) == Empty {
				lastMoveBonus += midgameLastMoveWeight
			}
		}
	}
	isJump := 0.0
	if b.LastMove.IsJump() {
		isJump = 1.0
	}

	// 特征向量按顺序返回：
	// [r, myCnt-opCnt, outer, danger, mobDiff, infDiffWeighted,
	//  holesPenalty, openingPenalty, earlyJumpCost, lastMoveBonus]
	return []float64{
		r,
		float64(myCnt - opCnt),
		float64(outer),
		float64(danger),
		float64(mobDiff),
		float64(infDiffWeighted),
		float64(holesPenalty),
		float64(openingPenalty),
		float64(earlyJumpCost),
		float64(lastMoveBonus),
		isJump,
	}
}

// Predict 用线性模型估值。注意：ExtractFeatures 就是之前加好的那 11 维特征函数
func Predict(b *Board, player CellState) int {
	feats := ExtractFeatures(b, player)
	sum := learnedB
	for i, f := range feats {
		sum += learnedW[i] * f
	}
	// 四舍五入到整数
	return int(math.Round(sum))
}
