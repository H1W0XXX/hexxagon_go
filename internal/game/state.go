package game

import (
	"errors"
)

// GameState 包含了整个游戏的状态，包括棋盘、当前玩家、分数和胜负状态
type GameState struct {
	Board         *Board    // 棋盘
	CurrentPlayer CellState // 当前玩家 (PlayerA 或 PlayerB)
	ScoreA        int       // 玩家 A 的分数
	ScoreB        int       // 玩家 B 的分数
	GameOver      bool      // 游戏是否结束
	Winner        CellState // 胜者 (PlayerA、PlayerB 或 Empty 表示平局)
}

// NewGameState 创建并初始化一个新的游戏状态，radius 是棋盘半径
// 默认在六边形的三个角放置玩家 A 的棋子，在相对三个角放置玩家 B 的棋子
func NewGameState(radius int) *GameState {
	// 创建空棋盘
	b := NewBoard(radius)
	// 角落坐标 (A 方)
	cornersA := []HexCoord{
		{radius, 0},
		{0, -radius},
		{-radius, radius},
	}
	// 对立角坐标 (B 方)
	cornersB := []HexCoord{
		{-radius, 0},
		{0, radius},
		{radius, -radius},
	}
	// 放置初始棋子
	for _, c := range cornersA {
		_ = b.Set(c, PlayerA)
	}
	for _, c := range cornersB {
		_ = b.Set(c, PlayerB)
	}
	// 构造 GameState
	gs := &GameState{
		Board:         b,
		CurrentPlayer: PlayerA,
	}
	gs.updateScores() // 计算初始分数
	return gs
}

// updateScores 重新统计棋子数量，更新 ScoreA 和 ScoreB
func (gs *GameState) updateScores() {
	a, b := 0, 0
	for _, coord := range gs.Board.AllCoords() {
		switch gs.Board.Get(coord) {
		case PlayerA:
			a++
		case PlayerB:
			b++
		}
	}
	gs.ScoreA = a
	gs.ScoreB = b
}

// MakeMove 尝试执行一次玩家移动，并自动处理翻转、分数更新、切换回合和结束判定
func (gs *GameState) MakeMove(m Move) (int, error) {
	if gs.GameOver {
		return 0, errors.New("游戏已结束，无法继续移动")
	}
	// 执行移动和感染
	infected, err := m.Apply(gs.Board, gs.CurrentPlayer)
	if err != nil {
		return 0, err
	}
	// 更新分数
	gs.updateScores()
	// 检查游戏是否结束
	gs.checkGameOver()
	// 如果未结束，切换到下一个玩家
	if !gs.GameOver {
		gs.CurrentPlayer = Opponent(gs.CurrentPlayer)
	}
	return infected, nil
}

// checkGameOver 判断游戏是否结束：
// 1) 棋盘无空位；或 2) 当前玩家无任何合法移动
func (gs *GameState) checkGameOver() {
	// 如果已经结束，直接返回
	if gs.GameOver {
		return
	}

	// 统计棋盘上各状态数量
	var countA, countB, emptyCount int
	for _, coord := range gs.Board.AllCoords() {
		switch gs.Board.Get(coord) {
		case PlayerA:
			countA++
		case PlayerB:
			countB++
		case Empty:
			emptyCount++
		}
	}

	// 计算双方是否还有合法走法
	noMovesA := len(GenerateMoves(gs.Board, PlayerA)) == 0
	noMovesB := len(GenerateMoves(gs.Board, PlayerB)) == 0

	// 结束条件：棋盘无空格，或任一方无棋子，或任一方无合法走法
	if emptyCount == 0 || countA == 0 || countB == 0 || noMovesA || noMovesB {
		gs.GameOver = true
		// 最终统计一次分数（可选，如果你在其他地方也维护了 ScoreA/B）
		gs.updateScores()

		// 判断胜利者：
		//  - 如果 PlayerA 无棋子或无路可走，PlayerB 胜
		//  - 如果 PlayerB 无棋子或无路可走，PlayerA 胜
		//  - 否则按分数多的一方胜，平局时 Winner 置 Empty
		switch {
		case countA == 0 || noMovesA:
			gs.Winner = PlayerB
		case countB == 0 || noMovesB:
			gs.Winner = PlayerA
		default:
			if gs.ScoreA > gs.ScoreB {
				gs.Winner = PlayerA
			} else if gs.ScoreB > gs.ScoreA {
				gs.Winner = PlayerB
			} else {
				gs.Winner = Empty // 平局
			}
		}
	}
}

// GetScores 返回当前双方的分数 (A, B)
func (gs *GameState) GetScores() (int, int) {
	return gs.ScoreA, gs.ScoreB
}

// Reset 重置游戏到初始状态，保留相同半径
func (gs *GameState) Reset() {
	radius := gs.Board.radius
	newGs := NewGameState(radius)
	*gs = *newGs
}
