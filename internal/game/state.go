package game

import (
	"errors"
	"fmt"
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
func (gs *GameState) MakeMove(m Move) ([]HexCoord, undoInfo, error) {
	if gs.GameOver {
		return nil, undoInfo{}, errors.New("游戏已结束")
	}
	infectedCoords, undo := m.MakeMove(gs.Board, gs.CurrentPlayer)
	gs.updateScores()
	gs.checkGameOver()
	if !gs.GameOver {
		gs.CurrentPlayer = Opponent(gs.CurrentPlayer)
	}
	return infectedCoords, undo, nil
}

// checkGameOver 判断游戏是否结束：
// 1) 棋盘无空位；或 2) 当前玩家无任何合法移动
func (gs *GameState) checkGameOver() {
	// 如果已经结束，直接返回
	if gs.GameOver {

		return
	}
	// 先把所有被围的小块“吃”掉
	gs.fillEnclosedRegions()
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
	if emptyCount == 0 || countA == 0 || countB == 0 || (noMovesA && noMovesB) {
		gs.GameOver = true
		// 最终统计一次分数
		gs.updateScores()

		// 判断胜利者
		switch {
		case countA == 0:
			gs.Winner = PlayerB
		case countB == 0:
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

		// **在这里输出双方子数和胜者**
		fmt.Printf("游戏结束！玩家 A: %d 个棋子，玩家 B: %d 个棋子。", gs.ScoreA, gs.ScoreB)
		switch gs.Winner {
		case PlayerA:
			fmt.Println("胜者：玩家 A")
		case PlayerB:
			fmt.Println("胜者：玩家 B")
		default:
			fmt.Println("平局！")
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

func (gs *GameState) fillEnclosedRegions() {
	radius := gs.Board.radius
	visited := make(map[HexCoord]bool)

	// 遍历所有空格
	for _, start := range gs.Board.AllCoords() {
		if gs.Board.Get(start) != Empty || visited[start] {
			continue
		}

		// BFS 收集这个空格连通区域
		queue := []HexCoord{start}
		region := []HexCoord{start}
		visited[start] = true

		touchesBorder := false
		borderStates := make(map[CellState]bool)

		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]

			// 只要有一个格子在最外层，就不是封闭区
			if abs(cur.Q) == radius || abs(cur.R) == radius || abs(cur.Q+cur.R) == radius {
				touchesBorder = true
			}

			// 遍历相邻六个方向
			for _, nb := range gs.Board.Neighbors(cur) {
				switch s := gs.Board.Get(nb); s {
				case Empty:
					if !visited[nb] {
						visited[nb] = true
						queue = append(queue, nb)
						region = append(region, nb)
					}
				case PlayerA, PlayerB:
					borderStates[s] = true
				}
			}
		}

		// 如果区域不连边界，且只被 A 或只被 B 包围
		if !touchesBorder && len(borderStates) == 1 {
			var owner CellState
			for p := range borderStates {
				owner = p
			}

			// 把这个区域的所有空格，都当作 owner 的子
			for _, c := range region {
				_ = gs.Board.Set(c, owner)
			}
		}
	}
}
