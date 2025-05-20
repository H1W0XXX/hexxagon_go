package ui

import (
	"fmt"
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
	// 只在鼠标左键刚按下时响应
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return
	}
	mx, my := ebiten.CursorPosition()
	//fmt.Println("mx,my")
	//fmt.Println(mx, my)
	coord, ok := pixelToAxial(float64(mx), float64(my), gs.state.Board, gs.tileImage)

	if !ok {
		gs.audioManager.Play("cancel_select_piece")
		return
	}

	// 当前玩家
	player := gs.state.CurrentPlayer

	// 尚未选中来源格，则尝试选中自己的棋子
	if gs.selected == nil {
		if gs.state.Board.Get(coord) == player {
			gs.selected = &coord
			gs.audioManager.Play("select_piece") // 选棋音效
		} else {
			gs.audioManager.Play("cancel_select_piece") // 无效选棋
		}
		return
	}

	// 已选中来源格，coord 为目标格，尝试走子
	move := game.Move{From: *gs.selected, To: coord}
	infected, err := gs.state.MakeMove(move)
	if err != nil {
		// 如果目标是自己的棋子，则重新选中
		if gs.state.Board.Get(coord) == player {
			gs.selected = &coord
			gs.audioManager.Play("select_piece")
		} else {
			// 无效移动，取消选中
			gs.selected = nil
			gs.audioManager.Play("cancel_select_piece")
		}
		return
	}

	// 移动成功后，根据是分裂(Clone)还是跳跃(Jump)播放相应音效
	// 1) 先把分裂/跳跃的音效 key 放到 slice 里
	var seq []string
	total := time.Duration(0)
	if move.IsClone() {
		if player == game.PlayerA {
			seq = append(seq, "red_split")
			total += 966 * time.Millisecond
		} else {
			seq = append(seq, "white_split")
			total += 470 * time.Millisecond
		}
	} else if move.IsJump() {
		if player == game.PlayerA {
			seq = append(seq, "red_split") //分裂和跳跃用的相同音响
			total += 966 * time.Millisecond
		} else {
			seq = append(seq, "white_jump")
			total += 496 * time.Millisecond
		}
	}

	// 2) 如果有感染，就把捕获音效也加入队列
	if infected > 0 {
		if player == game.PlayerA {
			seq = append(seq, "red_capture_white_before")
			total += 287 * time.Millisecond
			seq = append(seq, "red_capture_white_after")
			total += 757 * time.Millisecond

		} else {
			seq = append(seq, "white_capture_red_before")
			fmt.Println("white_capture_red_before")
			total += 653 * time.Millisecond
			seq = append(seq, "white_capture_red_after")
			total += 548 * time.Millisecond
		}
	}
	seq = append(seq, "all_capture_after")
	//fmt.Println(seq)
	// 3) 最后一次性调用 PlaySequential
	gs.audioManager.PlaySequential(seq...)
	total += 50 * time.Millisecond
	gs.aiDelayUntil = time.Now().Add(total)
	// 走子完成，取消选中
	gs.selected = nil
}
