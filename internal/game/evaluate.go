// evaluate.go
package game

//var nn *nnue.Net

//func init() { nn, _ = nnue.Load("assets/nnue.npz.bin") }

//func evaluate(b *Board, player CellState) int {
//	feat := b.ToFeature(player)
//	return int(nn.Eval(feat) * 5)
//}

// evaluate：棋子差 × 8  +  机动性差 × 2  +  当前感染潜力 × 1  +  中心控制
// 改完后的权重：棋子差×10 + 机动性差×2 + 感染潜力×2 + 中心控制×1
// evaluate：棋子差 ×14  +  机动性差 ×2  +  感染差 ×2  +  外缘加分
// ----- 参数区，可修改这两个阈值 ------
// 当“空位比例” r <= cloneThresh 时，分裂权重 clamp 到最小值。
// 当“空位比例” r <= jumpThresh  时，跳跃权重 clamp 到最大值。
var (
	cloneThresh = 0.25      // 默认 1/4
	jumpThresh  = 1.0 / 3.0 // 默认 1/3
)

// 加分上下限
const (
	CLONE_BONUS_MAX = 40
	CLONE_BONUS_MIN = 14
	JUMP_BONUS_MAX  = 14
	JUMP_BONUS_MIN  = 0
)

// evaluate：棋子差 × 动态分裂权重 + 机动性差×2 + 感染差×2 + 外缘加分
func evaluate(b *Board, player CellState) int {
	op := Opponent(player)

	// 1) 计算空位比例 r
	coords := b.AllCoords()
	total := len(coords)
	empties := 0
	for _, c := range coords {
		if b.Get(c) == Empty {
			empties++
		}
	}
	r := float64(empties) / float64(total) // 1→0

	// 2) 动态分裂权重 pieceW: r∈[cloneThresh,1] 线性映射到 [CLONE_BONUS_MIN,CLONE_BONUS_MAX]
	var pieceW int
	if r >= cloneThresh {
		t := (r - cloneThresh) / (1.0 - cloneThresh)
		pieceW = int(t*float64(CLONE_BONUS_MAX-CLONE_BONUS_MIN) + float64(CLONE_BONUS_MIN) + 0.5)
	} else {
		pieceW = CLONE_BONUS_MIN
	}

	// 3) 动态跳跃权重 jumpW: r∈[jumpThresh,1] 逆向映射 [JUMP_BONUS_MAX→JUMP_BONUS_MIN]
	var jumpW int
	if r <= jumpThresh {
		jumpW = JUMP_BONUS_MAX
	} else if r < 1.0 {
		t := (1.0 - r) / (1.0 - jumpThresh)
		jumpW = int(t*float64(JUMP_BONUS_MAX-JUMP_BONUS_MIN) + float64(JUMP_BONUS_MIN) + 0.5)
	} else {
		jumpW = JUMP_BONUS_MIN
	}

	// 4) 统计棋子差 & 外缘分
	origin := HexCoord{0, 0}
	outerScore := 0
	myCount, oppCount := 0, 0
	for _, c := range coords {
		d := hexDistance(c, origin)
		s := b.Get(c)
		switch s {
		case player:
			myCount++
			if d == b.radius {
				outerScore++
			}
		case op:
			oppCount++
			if d == b.radius {
				outerScore--
			}
		}
	}
	pieceDiff := myCount - oppCount

	// 5) 机动性差
	myMob := len(GenerateMoves(b, player))
	opMob := len(GenerateMoves(b, op))
	mobDiff := myMob - opMob

	// 6) 感染差
	maxInf := func(pl CellState) int {
		best := 0
		for _, m := range GenerateMoves(b, pl) {
			if cnt, _ := m.ApplyPreview(b, pl); cnt > best {
				best = cnt
			}
		}
		return best
	}
	infDiff := maxInf(player) - maxInf(op)

	// 7) 合并加权
	// 这里只用 pieceW；在 pickMove 中可以额外用 jumpW 区分跳跃
	return pieceDiff*pieceW +
		mobDiff*jumpW +
		infDiff*2 +
		outerScore
}

//func Evaluate(b *Board, player CellState) int {
//	feat := b.ToFeature(player)
//	return int(nn.Eval(feat) * 5)
//}

// Evaluate 对外导出启发式评估，用于快速走子（非深度搜索）。
func Evaluate(b *Board, player CellState) int {
	return evaluate(b, player)
}

func (m Move) ApplyPreview(b *Board, player CellState) (infected int, ok bool) {
	// 调用 MakeMove 拿到被感染的坐标列表和 undo 信息
	infectedCoords, undo := m.MakeMove(b, player)
	// 撤销
	b.UnmakeMove(undo)
	// 返回感染数量
	return len(infectedCoords), true
}

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
