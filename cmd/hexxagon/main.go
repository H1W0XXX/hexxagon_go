package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"hexxagon_go/internal/ui"
)

func main() {
	const (
		screenW    = 800
		screenH    = 600
		sampleRate = 44100
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

	ebiten.SetWindowSize(screenW, screenH)
	ebiten.SetWindowTitle("Hexxagon")

	if err := ebiten.RunGame(screen); err != nil {
		log.Fatal(err)
	}
}
