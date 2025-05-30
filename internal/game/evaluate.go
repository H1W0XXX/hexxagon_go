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
	CLONE_BONUS_MAX = 20
	CLONE_BONUS_MIN = 14
	JUMP_BONUS_MAX  = 2
	JUMP_BONUS_MIN  = 0
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

// evaluate：合并所有评分项
func evaluate(b *Board, player CellState) int {
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
	edgeW := dynamicEdgeW(r)
	// 2) 动态权重
	pieceW := dynamicPieceW(r)
	jumpW := dynamicJumpW(r)
	infW := dynamicInfW(r)

	// 3) 统计棋子数、外缘、风险
	myCnt, opCnt := 0, 0
	outer, danger := 0, 0
	//origin := HexCoord{0, 0}
	for _, c := range coords {
		s := b.Get(c)
		if s == Empty {
			continue
		}
		onEdge := isOuter(c, b.radius)
		if s == player {
			myCnt++
			if onEdge {
				outer++
			}
			if isInOpponentRange(b, c, op) {
				danger++
			}
		} else {
			opCnt++
			if onEdge {
				outer--
			}
		}
	}

	//hole := 0
	//for _, c := range coords {
	//	if b.Get(c) != Empty {
	//		continue
	//	}
	//	// 空格至少有一个对手邻居
	//	hasEnemy := false
	//	for _, nb := range b.Neighbors(c) {
	//		if b.Get(nb) == op {
	//			hasEnemy = true
	//			break
	//		}
	//	}
	//	if hasEnemy {
	//		hole++
	//	}
	//}
	pieceScore := (myCnt - opCnt) * pieceW
	safetyScore := -danger * dangerW

	// 4) 机动性差
	mobDiff := len(GenerateMoves(b, player)) - len(GenerateMoves(b, op))
	// 5) 感染差
	infDiff := maxInf(b, player) - maxInf(b, op)

	// ---------- 6) 危险空洞惩罚 ----------
	holeW := 10 // 每个潜在“跳入大窟窿”扣 5 分，可再调大
	riskHoles := 0
	for _, c := range coords {
		if b.Get(c) != Empty {
			continue
		}
		// 条件 a：对手 <= 2 格可达
		reachable := false
		for _, nb := range b.AllCoords() {
			if b.Get(nb) == op && HexDist(nb, c) <= 2 {
				reachable = true
				break
			}
		}
		if !reachable {
			continue
		}
		// 条件 b：空洞周围至少 2 颗我方棋子
		ownAdj := 0
		for _, nb := range b.Neighbors(c) {
			if b.Get(nb) == player {
				ownAdj++
			}
		}
		if ownAdj >= 2 {
			riskHoles++
		}
	}
	riskPenalty := -riskHoles * holeW

	outerScore := outer * edgeW
	//fmt.Printf("outer=%d outerScore=%d piece=%d mob=%d inf=%d danger=%d total=%d\n",
	//	outer, outerScore,
	//	pieceScore, mobDiff*jumpW, infDiff*infW, safetyScore,
	//	pieceScore+mobDiff*jumpW+infDiff*infW+outerScore+safetyScore)
	// 6) 最终合分
	return pieceScore*2 +
		mobDiff*jumpW/2 +
		infDiff*infW/2 +
		outerScore +
		safetyScore +
		riskPenalty
}

// 对外导出
func Evaluate(b *Board, player CellState) int {
	return evaluate(b, player)
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
