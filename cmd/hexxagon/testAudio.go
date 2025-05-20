package main

import (
	"bytes"
	"embed"
	"fmt"
	"log"
	"time"

	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/mp3"
)

//go:embed audio/white_capture_red_before.mp3
var audioFS embed.FS

func main() {
	// 创建 AudioContext（标准采样率）
	const sampleRate = 44100
	ctx := audio.NewContext(sampleRate)

	// 读取文件内容
	data, err := audioFS.ReadFile("audio/white_capture_red_before.mp3")
	if err != nil {
		log.Fatalf("读取音频失败: %v", err)
	}

	// 解码 mp3
	stream, err := mp3.DecodeWithSampleRate(sampleRate, bytes.NewReader(data))
	if err != nil {
		log.Fatalf("MP3 解码失败: %v", err)
	}

	// 播放音频
	player, err := ctx.NewPlayer(stream)
	if err != nil {
		log.Fatalf("创建音频播放器失败: %v", err)
	}
	player.Play()

	fmt.Println("正在播放 white_capture_red_before.mp3...")

	// 等待音频播放完
	for player.IsPlaying() {
		time.Sleep(100 * time.Millisecond)
	}
}
