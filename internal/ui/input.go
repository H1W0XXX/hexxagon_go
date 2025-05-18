package ui

import (
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"hexxagon_go/internal/game"
)

// handleInput 处理鼠标点击事件，用于选中、移动并播放音效
func (gs *GameScreen) handleInput() {
	// 只在鼠标左键刚按下时响应
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return
	}
	// 获取鼠标屏幕坐标
	mx, my := ebiten.CursorPosition()
	fx, fy := float64(mx), float64(my)

	// 计算瓦片宽高及棋盘中心
	tileW, tileH := gs.tileImage.Bounds().Dx(), gs.tileImage.Bounds().Dy()
	centerX, centerY := float64(WindowWidth)/2, float64(WindowHeight)/2

	// 反向计算轴向坐标 (q, r)
	vs := float64(tileH) * math.Sqrt(3) / 2
	q := int(math.Round((fx - centerX) / (float64(tileW) * 0.75)))
	r := int(math.Round((fy-centerY)/vs - float64(q)/2))
	coord := game.HexCoord{Q: q, R: r}

	// 点击不在棋盘范围，播放取消选棋音效
	if !gs.state.Board.InBounds(coord) {
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
