package ui

import (
	"fmt"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"image/color"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"hexxagon_go/internal/assets"
	"hexxagon_go/internal/game"
)

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
	// 1) 每帧先清理已停止的播放器
	gs.audioManager.Update()

	// 2) 如果游戏已结束，直接返回
	if gs.state.GameOver {
		return nil
	}

	// 3) AI 回合：先检查音效是否在播，如果在播就先别走子
	if gs.state.CurrentPlayer == game.PlayerB {
		if gs.audioManager.Busy() {
			// 音效还没播完，什么都不做，下一帧再来
			return nil
		}
		if time.Now().Before(gs.aiDelayUntil) {
			return nil
		}
		// 音效播完了，真正执行 AI 落子
		move, ok := game.FindBestMove(gs.state.Board, game.PlayerB)
		if ok {
			infected, err := gs.state.MakeMove(move)
			if err == nil {
				// 分裂或跳跃音效
				if move.IsClone() {
					gs.audioManager.Play("white_split")
				} else {
					gs.audioManager.Play("white_jump")
				}
				// 吃子音效
				if infected > 0 {
					gs.audioManager.Play("white_capture_red_after")
				}
			}
		}
		// 清除选中状态
		gs.selected = nil
		// 一帧只走一步 AI，直接返回
		return nil
	}

	// 4) 人类回合：处理输入
	gs.handleInput()
	return nil
}

// Draw 每帧渲染：先清空背景，再绘制棋盘与棋子
func (gs *GameScreen) Draw(screen *ebiten.Image) {
	screen.Fill(color.Black)
	DrawBoardAndPiecesWithHints(
		screen,
		gs.state.Board,
		gs.tileImage,
		gs.hintGreenImage,
		gs.hintYellowImage,
		gs.pieceImages,
		gs.selected,
	)
}

// Layout 定义窗口尺寸
func (gs *GameScreen) Layout(outsideWidth, outsideHeight int) (int, int) {
	return WindowWidth, WindowHeight
}
