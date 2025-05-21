// File ui/input.go
package ui

import (
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"hexxagon_go/internal/assets"
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
	// 只在鼠标左键刚按下时响应
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
		if gs.state.Board.Get(coord) == player {
			gs.selected = &coord
			gs.audioManager.Play("select_piece")
		} else {
			gs.audioManager.Play("cancel_select_piece")
		}
		return
	}

	// 已选来源格，准备走子
	move := game.Move{From: *gs.selected, To: coord}
	infectedCoords, undo, err := gs.state.MakeMove(move)
	_ = undo
	if err != nil {
		// 走子失败，恢复选中逻辑
		if gs.state.Board.Get(coord) == player {
			gs.selected = &coord
			gs.audioManager.Play("select_piece")
		} else {
			gs.selected = nil
			gs.audioManager.Play("cancel_select_piece")
		}
		return
	}

	// —— 播放移动动画 —— //
	gs.addMoveAnim(move, player)

	// —— 计算移动动画的时长 —— //
	// 与 addMoveAnim 中使用的同样素材和 FPS
	dirKey := directionKey(move.From, move.To)
	var moveBase string
	switch {
	case move.IsClone() && player == game.PlayerA:
		moveBase = "redClone/" + dirKey
	case move.IsClone() && player == game.PlayerB:
		moveBase = "whiteClone/" + dirKey
	case move.IsJump() && player == game.PlayerA:
		moveBase = "redJump/" + dirKey
	case move.IsJump() && player == game.PlayerB:
		moveBase = "whiteJump/" + dirKey
	}
	frames := assets.AnimFrames[moveBase]
	const fps = 30
	// moveDur = 帧数 / FPS 秒
	moveDur := time.Duration(float64(len(frames)) / fps * float64(time.Second))

	// —— 延迟播放感染动画 —— //
	for _, inf := range infectedCoords {
		gs.addInfectAnim(move.To, inf, player, moveDur)
	}

	// —— 构造并播放音效队列 —— //
	var seq []string
	total := moveDur // 确保音效也延迟同样时长
	if move.IsClone() {
		if player == game.PlayerA {
			seq = append(seq, "red_split")
			total += 966 * time.Millisecond
		} else {
			seq = append(seq, "white_split")
			total += 470 * time.Millisecond
		}
	} else /* jump */ {
		if player == game.PlayerA {
			seq = append(seq, "red_split")
			total += 966 * time.Millisecond
		} else {
			seq = append(seq, "white_jump")
			total += 496 * time.Millisecond
		}
	}
	if len(infectedCoords) > 0 {
		if player == game.PlayerA {
			seq = append(seq, "red_capture_white_before")
			total += 287 * time.Millisecond
			seq = append(seq, "red_capture_white_after")
			total += 757 * time.Millisecond
		} else {
			seq = append(seq, "white_capture_red_before")
			total += 653 * time.Millisecond
			seq = append(seq, "white_capture_red_after")
			total += 548 * time.Millisecond
		}
	}
	seq = append(seq, "all_capture_after")
	// 延迟播放音效
	time.AfterFunc(moveDur, func() {
		gs.audioManager.PlaySequential(seq...)
	})

	// AI 或下一个玩家延迟
	gs.aiDelayUntil = time.Now().Add(total)
	gs.selected = nil
}
