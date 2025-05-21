package game

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

// 1) 把 Apply 改成返回被感染的坐标切片
func (m Move) Apply(b *Board, player CellState) ([]HexCoord, error) {
	// Validate & execute move 同旧逻辑……
	// （略）

	// 先收集原始棋盘上哪些邻居是对手
	opp := Opponent(player)
	var toBeInfected []HexCoord
	for _, n := range b.Neighbors(m.To) {
		if b.Get(n) == opp {
			toBeInfected = append(toBeInfected, n)
		}
	}

	// 然后再去改棋盘：jump/clone + 放置新棋子
	if m.IsJump() {
		if err := b.Set(m.From, Empty); err != nil {
			return nil, err
		}
	}
	if err := b.Set(m.To, player); err != nil {
		return nil, err
	}

	// 最后真正“感染”那些被收集到的格子
	for _, n := range toBeInfected {
		if err := b.Set(n, player); err != nil {
			return toBeInfected, err
		}
	}
	return toBeInfected, nil
}
