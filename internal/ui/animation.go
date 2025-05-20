// internal/ui/animation.go
package ui

import (
	"github.com/hajimehoshi/ebiten/v2"
	"hexxagon_go/internal/assets"
	"hexxagon_go/internal/game"
	"math"
	"time"
)

type FrameAnim struct {
	Frames []*ebiten.Image
	FPS    float64   // 每秒多少帧
	Start  time.Time // 动画开始时间
	Done   bool
	Coord  game.HexCoord // 要播放在哪个棋盘格
	Angle  float64       // 旋转角 (弧度)
}

func (a *FrameAnim) Current() *ebiten.Image {
	if a.Done || len(a.Frames) == 0 {
		return nil
	}
	elapsed := time.Since(a.Start).Seconds()
	idx := int(elapsed * a.FPS)
	if idx >= len(a.Frames) {
		a.Done = true
		return nil
	}
	return a.Frames[idx]
}

var dirAngle = map[[2]int]float64{
	{+1, 0}:  0,
	{+1, -1}: -math.Pi / 3,
	{0, -1}:  -2 * math.Pi / 3,
	{-1, 0}:  math.Pi,
	{-1, +1}: 2 * math.Pi / 3,
	{0, +1}:  math.Pi / 3,
}

func (gs *GameScreen) startInfectAnim(from, to game.HexCoord, player game.CellState) {
	dq := to.Q - from.Q
	dr := to.R - from.R
	key := [2]int{dq, dr}

	base := "afterRedInfectedByWhite"
	if player == game.PlayerB { // 白吃红，用另一套
		base = "whiteEatRed"
	}
	frames := assets.AnimFrames[base] // 左→右 素材

	anim := &FrameAnim{
		Frames: frames,
		FPS:    30,
		Start:  time.Now(),
		Coord:  to,            // 在被感染格播放
		Angle:  dirAngle[key], // 旋转角
	}
	gs.anims = append(gs.anims, anim)
}
