package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hexxagon_go/internal/game"
	"log"
	"os"
	"path/filepath"
	"time"
)

// dumpBoard 将棋盘状态序列化为 map，方便写入 JSON
func dumpBoard(b *game.Board) map[string]int {
	m := make(map[string]int)
	for _, c := range b.AllCoords() {
		key := fmt.Sprintf("(%d,%d)", c.Q, c.R)
		m[key] = int(b.Get(c))
	}
	return m
}

// countPieces 统计棋盘上属于 p 的子数
func countPieces(b *game.Board, p game.CellState) int {
	cnt := 0
	for _, c := range b.AllCoords() {
		if b.Get(c) == p {
			cnt++
		}
	}
	return cnt
}

func main() {
	numGames := flag.Int("n", 100, "有效对局数量，即收集到 n 局后停止")
	depth := flag.Int("d", 2, "搜索深度")
	outPrefix := flag.String("out", "selfplay", "输出文件名前缀，不含扩展名")
	flag.Parse()

	// 确保输出文件夹存在
	outDir := "selfplayData"
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalf("创建输出目录失败: %v", err)
	}

	type Step struct {
		Turn  int            `json:"turn"`
		Move  game.Move      `json:"move"`
		Board map[string]int `json:"board"`
	}
	type Match struct {
		Winner string `json:"winner"`
		Steps  []Step `json:"steps"`
	}

	const (
		minMoves = 50
		maxMoves = 500
	)

	valid := 0   // 已保存的有效对局数
	attempt := 0 // 尝试对局次数

	for valid < *numGames {
		attempt++
		log.Printf("开始第 %d 次尝试 (已收集 %d/%d 局有效对局)", attempt, valid, *numGames)

		state := game.NewGameState(4)
		player := game.PlayerA
		steps := []Step{}
		turn := 0
		aborted := false

		// 运行一局
		for {
			move, ok := game.FindBestMoveAtDepth(state.Board, player, *depth)
			if !ok {
				break
			}
			state.MakeMove(move)
			state.Board.LastMove = move

			steps = append(steps, Step{
				Turn:  turn,
				Move:  move,
				Board: dumpBoard(state.Board),
			})
			player = game.Opponent(player)
			turn++

			if turn >= maxMoves {
				log.Printf("尝试 %d 异常中止：超过 %d 步", attempt, maxMoves)
				aborted = true
				break
			}
		}

		// 丢弃不合格对局
		if aborted {
			continue
		}
		if turn < minMoves {
			log.Printf("尝试 %d 跳过：仅 %d 步 (<%d)", attempt, turn, minMoves)
			continue
		}

		// 确认胜负并保存
		scoreA := countPieces(state.Board, game.PlayerA)
		scoreB := countPieces(state.Board, game.PlayerB)
		winner := "draw"
		if scoreA > scoreB {
			winner = "A"
		} else if scoreB > scoreA {
			winner = "B"
		}
		log.Printf("第 %d 局有效对局完成: A=%d B=%d Winner=%s (步数=%d)",
			valid+1, scoreA, scoreB, winner, turn)

		match := Match{Winner: winner, Steps: steps}

		ts := time.Now().Format("20060102_150405")
		filename := fmt.Sprintf("%s_%s_%d.json", *outPrefix, ts, valid+1)
		fullpath := filepath.Join(outDir, filename)

		data, err := json.MarshalIndent(match, "", "  ")
		if err != nil {
			log.Printf("序列化第 %d 局出错: %v", valid+1, err)
			continue
		}
		if err := os.WriteFile(fullpath, data, 0644); err != nil {
			log.Printf("写入文件 %s 出错: %v", fullpath, err)
			continue
		}

		valid++
	}

	fmt.Println("自我对弈完成，输出目录:", outDir)
}

// go build -ldflags="-s -w" -gcflags="all=-trimpath=${PWD}" -asmflags="all=-trimpath=${PWD}" -o selfplay.exe .\cmd\selfplay\main.go
