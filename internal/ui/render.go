package ui

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"hexxagon_go/internal/game"
)

// DrawBoardAndPiecesWithHints 同时绘制棋盘和棋子，并对选中格的合法移动进行高亮显示
// tileImage: 普通空格贴图；greenHint: 近距复制移动高亮；yellowHint: 远距跳跃移动高亮；
// pieceImages: 玩家棋子贴图；selected: 当前选中的源格坐标（nil 表示未选中）。
func DrawBoardAndPiecesWithHints(
	screen *ebiten.Image,
	board *game.Board,
	tileImage, greenHint, yellowHint *ebiten.Image,
	pieceImages map[game.CellState]*ebiten.Image,
	selected *game.HexCoord,
) {
	// 计算瓦片和棋子尺寸
	tileW := float64(tileImage.Bounds().Dx())
	tileH := float64(tileImage.Bounds().Dy())
	vs := tileH * math.Sqrt(3) / 2 // 垂直间距
	centerX := float64(screen.Bounds().Dx()) / 2
	centerY := float64(screen.Bounds().Dy()) / 2

	// 合并玩家棋子底图
	combined := make(map[game.CellState]*ebiten.Image)
	for pl, img := range pieceImages {
		combined[pl] = createCombined(tileImage, img)
	}

	// 如果有选中，计算可落子目标集合
	var cloneTargets = make(map[game.HexCoord]struct{})
	var jumpTargets = make(map[game.HexCoord]struct{})
	if selected != nil {
		moves := game.GenerateMoves(board, board.Get(*selected))
		for _, m := range moves {
			if m.From == *selected {
				if m.IsClone() {
					cloneTargets[m.To] = struct{}{}
				} else if m.IsJump() {
					jumpTargets[m.To] = struct{}{}
				}
			}
		}
	}

	// 遍历所有格子并绘制
	for _, coord := range board.AllCoords() {
		// 计算格子中心屏幕坐标
		x := centerX + float64(coord.Q)*tileW*0.75
		y := centerY + (float64(coord.R)+float64(coord.Q)/2)*vs
		x = math.Round(x)
		y = math.Round(y)

		// 选择贴图
		var img *ebiten.Image
		s := board.Get(coord)
		if s == game.PlayerA || s == game.PlayerB {
			// 已有棋子，使用合成图
			img = combined[s]
		} else if selected != nil {
			// 空格但有提示
			if _, ok := cloneTargets[coord]; ok {
				img = greenHint
			} else if _, ok := jumpTargets[coord]; ok {
				img = yellowHint
			} else {
				img = tileImage
			}
		} else {
			// 空格且未选中
			img = tileImage
		}

		// 绘制到屏幕，中心对齐
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(x-tileW/2, y-tileH/2)
		screen.DrawImage(img, op)
	}
}

// createCombined 将格子底图与棋子图合并，棋子居中于格子中央
func createCombined(tileImg, pieceImg *ebiten.Image) *ebiten.Image {
	w, h := tileImg.Bounds().Dx(), tileImg.Bounds().Dy()
	img := ebiten.NewImage(w, h)
	img.DrawImage(tileImg, nil)
	// 棋子偏移到中央
	pw, ph := pieceImg.Bounds().Dx(), pieceImg.Bounds().Dy()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(w-pw)/2, float64(h-ph)/2)
	img.DrawImage(pieceImg, op)
	return img
}
