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
	CLONE_BONUS_MAX = 30
	CLONE_BONUS_MIN = 14
	JUMP_BONUS_MAX  = 2
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
	earlyJumpPenalty = 20
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

	// 2) 如果处于“开局前期”（r ≥ openingPhaseThresh），并且对手棋子数比自己多，就严重惩罚
	myCnt, opCnt := 0, 0
	for _, c := range coords {
		s := b.Get(c)
		if s == player {
			myCnt++
		} else if s == op {
			opCnt++
		}
	}
	openingPenalty := 0
	if r >= openingPhaseThresh && opCnt > myCnt {
		openingPenalty = (opCnt - myCnt) * openingPenaltyWeight
	}

	// 3) 动态权重计算
	edgeW := dynamicEdgeW(r)
	pieceW := dynamicPieceW(r)
	jumpW := dynamicJumpW(r)
	infW := dynamicInfW(r)

	// 4) 统计棋子数、外缘数量、危险数量
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
		} else {
			if onEdge {
				outer-- // 对手在边缘计负分
			}
		}
	}

	// 5) “机动性差”改为只统计 克隆 走法（mobCloneDiff），而不是 所有 走法
	//
	//    这样开局阶段 r ≥ openingPhaseThresh 时，会只关心“克隆”的数量，
	//    如果对手有更多可用克隆，也会被扣分；反之我方有更多可用克隆会加分。
	myMoves := GenerateMoves(b, player)
	opMoves := GenerateMoves(b, op)

	// 只统计 克隆 走法 数量
	myCloneCount := 0
	for _, m := range myMoves {
		if m.IsClone() {
			myCloneCount++
		}
	}
	opCloneCount := 0
	for _, m := range opMoves {
		if m.IsClone() {
			opCloneCount++
		}
	}
	cloneMobDiff := myCloneCount - opCloneCount

	// 如果不在开局阶段，则仍旧用“克隆+跳跃”之和做机动性差
	fullMobDiff := len(myMoves) - len(opMoves)

	// 6) “加权感染差”（infDiffWeighted）：对“跳跃感染”打低分，对“克隆感染”打高分
	infDiffWeighted := maxWeightedInfFromMoves(b, player, myMoves) -
		maxWeightedInfFromMoves(b, op, opMoves)

	// 7) “早期跳跃惩罚”：如果 r ≥ openingPhaseThresh，且我方存在可用克隆（myCloneCount>0），
	//    但静态评价中还是让 jump 得分部分占比很高（也就是说实际局面没有感染产生），
	//    就额外扣掉一笔 earlyJumpPenalty。
	//
	//    具体判断方式：只要开局阶段 r ≥ openingPhaseThresh && myCloneCount > 0 && infDiffWeighted == 0，
	//    说明“有克隆可以感染，却没有任何感染量（infDiffWeighted 为 0）”，
	//    那很可能就是 AI 那一步选择了“跳跃”而不感染，强行把棋子移到更远位置去。
	//    因此给出一次额外惩罚，让 AI 更倾向于用克隆去感染。
	earlyJumpCost := 0
	if r >= openingPhaseThresh && myCloneCount > 0 && infDiffWeighted == 0 {
		earlyJumpCost = earlyJumpPenalty
	}

	// 8) 边缘分
	outerScore := outer * edgeW

	// 9) 危险空洞惩罚
	holesPenalty := -evaluateHoles(b, player)

	// 10) 组合最终得分，把“开局被感染惩罚”和“早期跳跃惩罚”都扣掉
	//
	//     - infection 部分用 infDiffWeighted * infW
	//     - mobility 在开局阶段用 cloneMobDiff、其他阶段用 fullMobDiff
	//     - pieceScore = (我方棋子数 – 对手棋子数) * pieceW
	//     - safetyScore = - danger * dangerW
	pieceScore := (myCnt - opCnt) * pieceW
	safetyScore := -danger * dangerW

	// 根据是否跨过开局阶段来决定用哪个 mobility 差值
	mobScore := 0
	if r >= openingPhaseThresh {
		mobScore = cloneMobDiff * jumpW / 3
	} else {
		mobScore = fullMobDiff * jumpW / 3
	}

	finalScore :=
		pieceScore*2 +
			mobScore +
			infDiffWeighted*infW/2 +
			outerScore +
			safetyScore +
			holesPenalty -
			openingPenalty -
			earlyJumpCost

	return finalScore
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

func evaluateHoles(b *Board, player CellState) int {
	op := Opponent(player)
	visited := make(map[HexCoord]bool)
	holePenalty := 0
	holeWeight := 5 // 你可以视情况调大或调小

	for _, start := range b.AllCoords() {
		// 只关心还没访问的、而且是空的那个点
		if b.Get(start) != Empty || visited[start] {
			continue
		}

		// 用 BFS 把从 start 开始的整个连通空洞区域都找出来
		queue := []HexCoord{start}
		region := []HexCoord{start}
		visited[start] = true

		touchesBorder := false // 如果连通区域能连到“棋盘边缘”，我们可以选择不惩罚它
		// （或者你想只惩罚内侧空洞，就把 touchesBorder=true 当作“不是封闭空洞”：不扣分）

		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]

			// 判断是否连到最外圈(单纯作为示例，可以不判断)
			if abs(cur.Q) == b.radius || abs(cur.R) == b.radius || abs(cur.Q+cur.R) == b.radius {
				touchesBorder = true
			}

			// 扫描该点四周相邻的空格
			for _, nb := range b.Neighbors(cur) {
				if !visited[nb] && b.Get(nb) == Empty {
					visited[nb] = true
					queue = append(queue, nb)
					region = append(region, nb)
				}
			}
		}

		// 如果整个区域连到棋盘外缘，你也可以不惩罚，或者惩罚更少。
		// 这里我们假设：只惩罚不连边缘的“内侧空洞”。
		if touchesBorder {
			continue
		}

		// 至此，region 就是一整片连通的“空洞”。
		// 接下来要判断：对手有没有可能 1 步或者 2 步进入 region 中的某一个空格。
		// 如果对手从某个己方棋子位置出发，在 <=2 步内能跳到 region 里的某个格子，
		// 那就说明这个空洞对对手是“可乘之机”，我们就要按 size 扣分。

		regionSize := len(region) // 先记录区域大小
		opponentCanReach := false

		// 把对手所有棋子的位置先搜到一个切片里：
		var oppPositions []HexCoord
		for _, c := range b.AllCoords() {
			if b.Get(c) == op {
				oppPositions = append(oppPositions, c)
			}
		}

		// 对每个空洞中的点，都检查它和所有对手棋子之间的距离，
		// 只要发现有一个对手棋子 d<=2，就认为“对手可达”
		for _, holeCell := range region {
			if opponentCanReach {
				break
			}
			for _, opp := range oppPositions {
				if HexDist(opp, holeCell) <= 2 {
					opponentCanReach = true
					break
				}
			}
		}

		if opponentCanReach {
			// 按区域大小 * 权重来惩罚
			holePenalty += regionSize * holeWeight
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
