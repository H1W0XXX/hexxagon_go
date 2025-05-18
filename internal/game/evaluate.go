// evaluate.go
package game

// evaluate：棋子差 × 8  +  机动性差 × 2  +  当前感染潜力 × 1  +  中心控制
// 改完后的权重：棋子差×10 + 机动性差×2 + 感染潜力×2 + 中心控制×1
func evaluate(b *Board, player CellState) int {
	opp := Opponent(player)
	origin := HexCoord{0, 0}
	maxDist := b.radius

	var myPieces, oppPieces, centerScore int

	for _, c := range b.AllCoords() {
		w := maxDist - hexDistance(c, origin) // 0..radius
		switch b.Get(c) {
		case player:
			myPieces++
			centerScore += w
		case opp:
			oppPieces++
			centerScore -= w
		}
	}

	myMob := len(GenerateMoves(b, player))
	oppMob := len(GenerateMoves(b, opp))

	maxInf := func(pl CellState) int {
		best := 0
		for _, m := range GenerateMoves(b, pl) {
			if cnt, _ := m.ApplyPreview(b, pl); cnt > best {
				best = cnt
			}
		}
		return best
	}
	infDiff := maxInf(player) - maxInf(opp)

	return (myPieces-oppPieces)*10 + (myMob-oppMob)*2 + infDiff*2 + centerScore
}

func (m Move) ApplyPreview(b *Board, player CellState) (infected int, ok bool) {
	clone := b.Clone()
	infected, _ = m.Apply(clone, player)
	return
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
