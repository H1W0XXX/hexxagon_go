// File /ui/screen.go
package ui

import (
	"fmt"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"hexxagon_go/internal/assets"
	"hexxagon_go/internal/game"
)

var soundDurations = map[string]time.Duration{
	"white_split":              470 * time.Millisecond,
	"white_jump":               496 * time.Millisecond,
	"white_capture_red_before": 653 * time.Millisecond,
	"white_capture_red_after":  548 * time.Millisecond,
	"all_capture_after":        400 * time.Millisecond,
	// 如果还有别的 key 也记得加上
}

const (
	// 窗口尺寸
	WindowWidth  = 800
	WindowHeight = 600
	// 棋盘半径
	BoardRadius = 4
)

// GameScreen 实现 ebiten.Game 接口，管理游戏主循环和渲染
// selected 用于存储当前选中的源格
// TODO: 可添加 AI 相关字段，如自动落子间隔等

type GameScreen struct {
	state       *game.GameState                  // 游戏状态
	tileImage   *ebiten.Image                    // 棋盘格子贴图
	pieceImages map[game.CellState]*ebiten.Image // 棋子贴图映射
	selected    *game.HexCoord                   // 当前选中的源格
	// 高亮提示图
	hintGreenImage  *ebiten.Image // 复制移动近距离高亮图
	hintYellowImage *ebiten.Image // 跳跃移动远距离高亮图
	audioManager    *assets.AudioManager
	aiDelayUntil    time.Time
	offscreen       *ebiten.Image
	anims           []*FrameAnim // 正在播放的动画列表
}

// NewGameScreen 构造并初始化游戏界面
func NewGameScreen(ctx *audio.Context) (*GameScreen, error) {

	var err error
	gs := &GameScreen{
		state:       game.NewGameState(BoardRadius),
		pieceImages: make(map[game.CellState]*ebiten.Image),
		//audioManager: audioManager,
	}

	if gs.tileImage, err = assets.LoadImage("hex_space"); err != nil {
		return nil, err
	}
	if gs.pieceImages[game.PlayerA], err = assets.LoadImage("red_piece"); err != nil {
		return nil, err
	}
	if gs.pieceImages[game.PlayerB], err = assets.LoadImage("white_piece"); err != nil {
		return nil, err
	}
	if gs.hintGreenImage, err = assets.LoadImage("move_hint_green"); err != nil {
		return nil, err
	}
	if gs.hintYellowImage, err = assets.LoadImage("move_hint_yellow"); err != nil {
		return nil, err
	}

	if gs.audioManager, err = assets.NewAudioManager(ctx); err != nil {
		return nil, fmt.Errorf("初始化音频管理器失败: %w", err)
	}
	gs.offscreen = ebiten.NewImage(WindowWidth, WindowHeight)
	return gs, nil
}

// Update 每帧更新：处理用户输入和 AI（若有）
//
//	func (gs *GameScreen) Update() error {
//		// 处理玩家输入（选中/移动）
//		gs.audioManager.Update()
//		gs.handleInput()
//		// TODO: 如果是 AI 回合，可在此调用 AI 逻辑
//		return nil
//	}
func (gs *GameScreen) Update() error {
	gs.audioManager.Update()
	if gs.state.GameOver {
		return nil
	}

	// AI 回合
	if gs.state.CurrentPlayer == game.PlayerB {
		// 1) 如果还在延时中，先啥都不干
		if time.Now().Before(gs.aiDelayUntil) {
			return nil
		}

		// 2) 组音效队列
		move, ok := game.FindBestMove(gs.state.Board, game.PlayerB)
		if ok {
			infected, err := gs.state.MakeMove(move)
			if err == nil {
				var seq []string
				total := time.Duration(0)

				// 分裂/跳跃
				if move.IsClone() {
					seq = append(seq, "white_split")
					total += soundDurations["white_split"]
				} else {
					seq = append(seq, "white_jump")
					total += soundDurations["white_jump"]
				}

				// 吃子前后
				if infected > 0 {
					seq = append(seq, "white_capture_red_before")
					total += soundDurations["white_capture_red_before"]

					seq = append(seq, "white_capture_red_after")
					total += soundDurations["white_capture_red_after"]
				}

				// 完成后还能加个 all_capture_after
				seq = append(seq, "all_capture_after")
				total += soundDurations["all_capture_after"]

				// 最后加一点缓冲
				total += 50 * time.Millisecond

				// 3) 播放队列并设置下一次 AI 延时
				gs.audioManager.PlaySequential(seq...)
				gs.aiDelayUntil = time.Now().Add(total)
			}
		}

		gs.selected = nil
		return nil
	}

	// 人类回合
	gs.handleInput()
	return nil
}

// Draw 每帧渲染：先清空背景，再绘制棋盘与棋子
func (gs *GameScreen) Draw(screen *ebiten.Image) {
	// 1) 清空屏幕背景（window 上）
	screen.Fill(color.Black)

	// 2) 清空 offscreen 画布（800×600）
	gs.offscreen.Fill(color.Black)

	// 3) 所有棋盘+高亮+棋子都画到 offscreen
	DrawBoardAndPiecesWithHints(
		gs.offscreen,
		gs.state.Board,
		gs.tileImage,
		gs.hintGreenImage,
		gs.hintYellowImage,
		gs.pieceImages,
		gs.selected,
	)

	// 4) 把 offscreen 缩放、居中到 screen
	w, h := screen.Size()
	scaleX := float64(w) / float64(WindowWidth)
	scaleY := float64(h) / float64(WindowHeight)
	scale := math.Min(scaleX, scaleY)

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(scale, scale)
	dx := (float64(w) - float64(WindowWidth)*scale) / 2
	dy := (float64(h) - float64(WindowHeight)*scale) / 2
	op.GeoM.Translate(dx, dy)

	screen.DrawImage(gs.offscreen, op)
}

// Layout 定义窗口尺寸
func (gs *GameScreen) Layout(outsideWidth, outsideHeight int) (int, int) {
	return WindowWidth, WindowHeight
}

// return boardScale, originX, originY, tileW, tileH, vs
func boardTransform(tileImg *ebiten.Image) (float64, float64, float64, int, int, float64) {
	tileW := tileImg.Bounds().Dx()
	tileH := tileImg.Bounds().Dy()
	vs := float64(tileH) * math.Sqrt(3) / 2

	cols, rows := 2*BoardRadius+1, 2*BoardRadius+1
	boardW := float64(cols-1)*float64(tileW)*0.75 + float64(tileW)
	boardH := vs*float64(rows-1) + float64(tileH)

	scale := math.Min(float64(WindowWidth)/boardW, float64(WindowHeight)/boardH)
	originX := (float64(WindowWidth) - boardW*scale) / 2
	originY := (float64(WindowHeight) - boardH*scale) / 2
	return scale, originX, originY, tileW, tileH, vs
}
