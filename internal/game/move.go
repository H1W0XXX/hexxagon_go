package game

import "errors"

// Move 表示一次从 From 到 To 的走子
type Move struct {
	From HexCoord
	To   HexCoord
}

// cloneDirs 定义了 6 个相邻偏移（Distance == 1 的方向）
var cloneDirs = []HexCoord{
	{+1, 0},  // 东
	{+1, -1}, // 东北
	{0, -1},  // 西北
	{-1, 0},  // 西
	{-1, +1}, // 西南
	{0, +1},  // 东南
}

// jumpDirs 定义了 12 个跳跃偏移（Distance == 2 的方向）
// 这些偏移就是所有满足 |dq|+|dr|+|ds|=4 (hex 距离=2) 的组合
var jumpDirs = []HexCoord{
	{+2, 0}, {+2, -1}, {+2, -2},
	{+1, -2}, {0, -2}, {-1, -1},
	{-2, 0}, {-2, +1}, {-2, +2},
	{-1, +2}, {0, +2}, {+1, +1},
}

func Opponent(player CellState) CellState {
	switch player {
	case PlayerA:
		return PlayerB
	case PlayerB:
		return PlayerA
	}
	return Empty
}

// IsClone 返回这步是否是复制（复制：落点是距离 1 的相邻格子）
func (m Move) IsClone() bool {
	for _, d := range cloneDirs {
		if m.From.Q+d.Q == m.To.Q && m.From.R+d.R == m.To.R {
			return true
		}
	}
	return false
}

// IsJump 返回这步是否是跳跃（跳跃：落点是距离 2 的格子）
func (m Move) IsJump() bool {
	for _, d := range jumpDirs {
		if m.From.Q+d.Q == m.To.Q && m.From.R+d.R == m.To.R {
			return true
		}
	}
	return false
}

// GenerateMoves 枚举玩家 player 在棋盘 b 上所有合法走法
func GenerateMoves(b *Board, player CellState) []Move {
	var moves []Move
	// 遍历所有格子
	for _, c := range b.AllCoords() {
		if b.Get(c) != player {
			continue
		}
		// 1) 复制走法：6 个方向
		for _, d := range cloneDirs {
			to := HexCoord{c.Q + d.Q, c.R + d.R}
			if b.Get(to) == Empty {
				moves = append(moves, Move{From: c, To: to})
			}
		}
		// 2) 跳跃走法：12 个方向
		for _, d := range jumpDirs {
			to := HexCoord{c.Q + d.Q, c.R + d.R}
			if b.Get(to) == Empty {
				moves = append(moves, Move{From: c, To: to})
			}
		}
	}
	return moves
}

// Apply executes the move on the board for the given player.
// It places or moves the piece, then infects adjacent opponent pieces.
func (m Move) Apply(b *Board, player CellState) (int, error) {
	// Validate source
	if b.Get(m.From) != player {
		return 0, errors.New("source does not contain player's piece")
	}
	// Validate destination
	if b.Get(m.To) != Empty {
		return 0, errors.New("destination is not empty")
	}
	// Validate move distance
	if !(m.IsClone() || m.IsJump()) {
		return 0, errors.New("invalid move distance; must be clone (1) or jump (2)")
	}

	// Perform move
	if m.IsJump() {
		// Remove original piece
		if err := b.Set(m.From, Empty); err != nil {
			return 0, err
		}
	}
	// Place new or cloned piece
	if err := b.Set(m.To, player); err != nil {
		return 0, err
	}

	// Infect adjacent opponent pieces
	opp := Opponent(player)
	infectedCount := 0
	for _, n := range b.Neighbors(m.To) {
		if b.Get(n) == opp {
			if err := b.Set(n, player); err != nil {
				return infectedCount, err
			}
			infectedCount++
		}
	}
	return infectedCount, nil
}
