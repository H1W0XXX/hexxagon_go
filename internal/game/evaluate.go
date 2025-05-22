// file: internal/game/evaluate.go
package game

// 可调参数
var (
	cloneThresh = 0.25      // 克隆/跳跃阈值
	jumpThresh  = 1.0 / 3.0 // 跳跃/残局阈值
	dangerW     = 6         // 暴露惩罚权重
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
			if hexDistance(nb2, c) == 2 && b.Get(nb2) == opponent {
				return true
			}
		}
	}
	return false
}

// hexDistance：计算两个坐标的六边形距离
func hexDistance(a, b HexCoord) int {
	dq := abs(a.Q - b.Q)
	dr := abs(a.R - b.R)
	ds := abs((-a.Q - a.R) - (-b.Q - b.R))
	if dq >= dr && dq >= ds {
		return dq
	}
	if dr >= ds {
		return dr
	}
	return ds
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

	// 2) 动态权重
	pieceW := dynamicPieceW(r)
	jumpW := dynamicJumpW(r)
	infW := dynamicInfW(r)

	// 3) 统计棋子数、外缘、风险
	myCnt, opCnt := 0, 0
	outer, danger := 0, 0
	origin := HexCoord{0, 0}
	for _, c := range coords {
		s := b.Get(c)
		if s == Empty {
			continue
		}
		d := hexDistance(c, origin)
		if s == player {
			myCnt++
			if d == b.radius {
				outer++
			}
			if isInOpponentRange(b, c, op) {
				danger++
			}
		} else {
			opCnt++
			if d == b.radius {
				outer--
			}
		}
	}

	pieceScore := (myCnt - opCnt) * pieceW
	safetyScore := -danger * dangerW

	// 4) 机动性差
	mobDiff := len(GenerateMoves(b, player)) - len(GenerateMoves(b, op))
	// 5) 感染差
	infDiff := maxInf(b, player) - maxInf(b, op)

	// 6) 最终合分
	return pieceScore +
		mobDiff*jumpW +
		infDiff*infW +
		outer +
		safetyScore
}

// 对外导出
func Evaluate(b *Board, player CellState) int {
	return evaluate(b, player)
}
