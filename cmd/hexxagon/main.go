package main

import (
	"flag"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"golang.org/x/sys/windows"
	"hexxagon_go/internal/ui"
	"log"
	"runtime"
)

// import _ "net/http/pprof"
//
//	func init() {
//		if runtime.GOOS == "windows" {
//			h := windows.CurrentProcess()
//
//			// BELOW_NORMAL_PRIORITY_CLASS = 0x00004000
//			if err := windows.SetPriorityClass(h, windows.BELOW_NORMAL_PRIORITY_CLASS); err != nil {
//				log.Printf("设置进程优先级失败: %v", err)
//			} else {
//				log.Println("已将进程优先级设置为 BELOW_NORMAL")
//			}
//		}
//	}
func main() {
	const (
		screenW     = 800
		screenH     = 600
		sampleRate  = 44100
		ScreenScale = 1
	)

	// —— 新增：启动参数 —— //
	modeFlag := flag.String("mode", "pve", "游戏模式: pve(人机) 或 pvp(人人)")
	flag.Parse()
	aiEnabled := (*modeFlag == "pve") // pve=启用 AI，pvp=禁用 AI

	ctx := audio.NewContext(sampleRate)
	if ctx == nil {
		log.Fatal("audio context not initialized")
	}

	screen, err := ui.NewGameScreen(ctx, aiEnabled) // 传入开关
	if err != nil {
		log.Fatal(err)
	}

	ebiten.SetVsyncEnabled(true)
	ebiten.SetTPS(30)
	ebiten.SetWindowSize(screenW*ScreenScale, screenH*ScreenScale)
	ebiten.SetWindowTitle("Hexxagon")
	//go func() {
	//	log.Println(http.ListenAndServe("127.0.0.1:6060", nil))
	//}()
	if err := ebiten.RunGame(screen); err != nil {
		log.Fatal(err)
	}

}

// go build -ldflags="-s -w" -gcflags="all=-trimpath=${PWD}" -asmflags="all=-trimpath=${PWD}" -o hexAI.exe .\cmd\hexxagon\main.go
