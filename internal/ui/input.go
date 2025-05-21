// File ui/input.go
package ui

import (
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"hexxagon_go/internal/game"
)

func getBoardTransform(tileImg *ebiten.Image) (scale, orgX, orgY, tileW, tileH, vs float64) {
	tileW = float64(tileImg.Bounds().Dx())
	tileH = float64(tileImg.Bounds().Dy())
	vs = tileH * math.Sqrt(3) / 2

	cols := 2*BoardRadius + 1
	rows := 2*BoardRadius + 1
	boardW := float64(cols-1)*tileW*0.75 + tileW
	boardH := vs*float64(rows-1) + tileH

	scale = math.Min(float64(WindowWidth)/boardW, float64(WindowHeight)/boardH)
	orgX = (float64(WindowWidth) - boardW*scale) / 2
	orgY = (float64(WindowHeight) - boardH*scale) / 2
	return
}

func cubeRound(xf, yf, zf float64) (int, int, int) {
	rx := math.Round(xf)
	ry := math.Round(yf)
	rz := math.Round(zf)

	dx := math.Abs(rx - xf)
	dy := math.Abs(ry - yf)
	dz := math.Abs(rz - zf)

	if dx >= dy && dx >= dz {
		rx = -ry - rz
	} else if dy >= dz {
		ry = -rx - rz
	} else {
		rz = -rx - ry
	}
	return int(rx), int(ry), int(rz)
}

// pixelToAxial 把屏幕像素坐标反算成 (q,r)
func pixelToAxial(fx, fy float64, board *game.Board, tileImg *ebiten.Image) (game.HexCoord, bool) {
	scale, orgX, orgY, tileWf, tileHf, vs := getBoardTransform(tileImg)
	dx := tileWf * 0.75

	// 1. 去掉平移、缩放
	x := (fx - orgX) / scale
	y := (fy - orgY) / scale

	// 2. 再去掉把中心移到 (0,0)
	x -= float64(BoardRadius) * dx
	y -= float64(BoardRadius) * vs

	// *** 关键补偿：移回半个瓦片的中心 ***
	x -= tileWf / 2 // ← 新增
	y -= tileHf / 2 // ← 新增

	// 3. 浮点轴向
	qf := x / dx
	rf := y/vs - qf/2

	// 4. 立方整体取整
	xf, zf := qf, rf
	yf := -xf - zf
	rx, _, rz := cubeRound(xf, yf, zf)

	coord := game.HexCoord{Q: rx, R: rz}
	return coord, board.InBounds(coord)
}

// handleInput 处理鼠标点击事件，用于选中、移动并播放音效
func (gs *GameScreen) handleInput() {
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return
	}
	mx, my := ebiten.CursorPosition()
	coord, ok := pixelToAxial(float64(mx), float64(my), gs.state.Board, gs.tileImage)
	if !ok {
		gs.audioManager.Play("cancel_select_piece")
		return
	}

	player := gs.state.CurrentPlayer
	if gs.selected == nil {
		// 仅负责选中，不再做走子相关逻辑
		if gs.state.Board.Get(coord) == player {
			gs.selected = &coord
			gs.audioManager.Play("select_piece")
		} else {
			gs.audioManager.Play("cancel_select_piece")
		}
		return
	}

	move := game.Move{From: *gs.selected, To: coord}
	if total, err := gs.performMove(move, player); err != nil {
		// 走子失败，保持原有“重新选中/取消”逻辑
		if gs.state.Board.Get(coord) == player {
			gs.selected = &coord
			gs.audioManager.Play("select_piece")
		} else {
			gs.selected = nil
			gs.audioManager.Play("cancel_select_piece")
		}
		return
	} else {
		gs.aiDelayUntil = time.Now().Add(total)
		gs.selected = nil
	}
}
