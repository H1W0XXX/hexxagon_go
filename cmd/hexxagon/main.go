package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"hexxagon_go/internal/ui"
)

func main() {
	const (
		screenW     = 800
		screenH     = 600
		sampleRate  = 44100
		ScreenScale = 1
	)

	ctx := audio.NewContext(sampleRate)
	if ctx == nil {
		log.Fatal("audio context not initialized")
	}
	//fmt.Println(ctx)

	screen, err := ui.NewGameScreen(ctx)
	if err != nil {
		log.Fatal(err)
	}
	ebiten.SetVsyncEnabled(false) // 禁用VSync，手动控制帧率
	ebiten.SetTPS(30)             // 每秒逻辑更新次数限制为30
	ebiten.SetWindowSize(screenW, screenH)
	ebiten.SetWindowSize(screenW*ScreenScale, screenH*ScreenScale)
	ebiten.SetWindowTitle("Hexxagon")

	if err := ebiten.RunGame(screen); err != nil {
		log.Fatal(err)
	}
}

// go build -ldflags="-s -w" -gcflags="all=-trimpath=${PWD}" -asmflags="all=-trimpath=${PWD}" -o hexAI.exe .\cmd\hexxagon\main.go
