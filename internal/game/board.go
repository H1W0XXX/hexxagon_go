package game

import (
	"errors"
)

// CellState represents the state of a cell on the board.
// It can be Empty, Blocked, or occupied by PlayerA or PlayerB.
type CellState int

const (
	Empty CellState = iota
	Blocked
	PlayerA
	PlayerB
)

// HexCoord represents an axial hex coordinate (q, r).
type HexCoord struct {
	Q, R int
}

// Directions defines the 6 neighbor offsets in axial coordinates.
var Directions = []HexCoord{
	{1, 0}, {1, -1}, {0, -1},
	{-1, 0}, {-1, 1}, {0, 1},
}

// Board represents a hexagonal board of a given radius.
// Coordinates satisfying |q| <= radius, |r| <= radius, |q+r| <= radius are valid.
type Board struct {
	radius int
	cells  map[HexCoord]CellState
}

// NewBoard creates and initializes a new board with the given radius.
func NewBoard(radius int) *Board {
	b := &Board{
		radius: radius,
		cells:  make(map[HexCoord]CellState),
	}
	for q := -radius; q <= radius; q++ {
		for r := -radius; r <= radius; r++ {
			if abs(q)+abs(r)+abs(-q-r) <= 2*radius {
				b.cells[HexCoord{q, r}] = Empty
			}
		}
	}
	return b
}

// InBounds returns true if coord c is within the board's radius.
func (b *Board) InBounds(c HexCoord) bool {
	if abs(c.Q) > b.radius || abs(c.R) > b.radius || abs(-c.Q-c.R) > b.radius {
		return false
	}
	return true
}

// Get returns the cell state at coord c. If out of bounds, returns Blocked.
func (b *Board) Get(c HexCoord) CellState {
	if !b.InBounds(c) {
		return Blocked
	}
	return b.cells[c]
}

// Set updates the cell state at coord c. Returns an error if c is out of bounds.
func (b *Board) Set(c HexCoord, state CellState) error {
	if !b.InBounds(c) {
		return errors.New("coordinate out of bounds")
	}
	b.cells[c] = state
	return nil
}

// Neighbors returns all in-bounds neighbor coordinates of c.
func (b *Board) Neighbors(c HexCoord) []HexCoord {
	var result []HexCoord
	for _, d := range Directions {
		n := HexCoord{c.Q + d.Q, c.R + d.R}
		if b.InBounds(n) {
			result = append(result, n)
		}
	}
	return result
}

// AllCoords returns a slice of all coordinates on the board.
func (b *Board) AllCoords() []HexCoord {
	coords := make([]HexCoord, 0, len(b.cells))
	for c := range b.cells {
		coords = append(coords, c)
	}
	return coords
}

// abs returns the absolute value of x.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Clone 返回当前棋盘的深拷贝，用于 AI 模拟走子
func (b *Board) Clone() *Board {
	// 新建一个 cells map，并复制所有格子状态
	newCells := make(map[HexCoord]CellState, len(b.cells))
	for coord, state := range b.cells {
		newCells[coord] = state
	}
	// 返回一个新的 Board 实例
	return &Board{
		radius: b.radius,
		cells:  newCells,
	}
}
