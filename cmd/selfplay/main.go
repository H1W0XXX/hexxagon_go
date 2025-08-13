package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"hexxagon_go/internal/game"
	//"hexxagon_go/internal/ml"
	"io"
	"log"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

func main() {
	// ───── 参数 ─────
	numGames := flag.Int("n", 50000, "目标总对局数")
	depth := flag.Int("d", 2, "搜索深度")
	outFile := flag.String("out", "dataset.csv", "CSV 文件")
	flag.Parse()

	_ = game.AllCoords(4)

	// ───── 修复 CSV ─────
	done := repairCSV(*outFile, game.TensorLen+3)

	// ───── 打开文件 + Writer + 互斥锁 ─────
	f, err := os.OpenFile(*outFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("open csv: %v", err)
	}
	w := csv.NewWriter(f)
	var wMu sync.Mutex
	defer func() { w.Flush(); f.Close() }()

	// ───── 并发 worker 池 ─────
	workers := runtime.NumCPU() / 4
	if workers < 1 {
		workers = 1
	}
	log.Printf("CPU=%d，启动 %d 个 worker 并行自对弈", runtime.NumCPU(), workers)

	jobs := make(chan int, workers*2)
	var wg sync.WaitGroup
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID))) // 独立随机源

			for id := range jobs { // ← 这里把 id 取出来
				rows, ok := playOneGame(*depth, id, r) // 把 id 和随机源传进去
				if !ok {
					continue
				}

				wMu.Lock()
				for _, row := range rows {
					w.Write(row)
				}
				w.Flush()
				wMu.Unlock()
			}
		}(i)
	}

	// ───── 投任务 ─────
	for g := done; g < *numGames; g++ {
		jobs <- g
		if (g+1-done)%100 == 0 {
			log.Printf("投放进度 %d/%d", g+1-done, *numGames-done)
		}
	}
	close(jobs)
	wg.Wait()
}

/*
playOneGame 把“一整局合法且 50≤步数≤500 的样本”转换为 [][]string。

	返回 ok=false 表示该局被丢弃（步数过短/过长）。
*/
func playOneGame(depth int, id int, r *rand.Rand) ([][]string, bool) {
	const (
		maxMoves = 500
		minMoves = 50
	)
	state := game.NewGameState(4)
	addRandomOpening(state, 2, r)

	player := game.PlayerA

	var rows [][]string
	moves := 0
	for {
		curDepth := depth
		if id%2 == 0 && player == game.PlayerB && depth > 1 {
			curDepth = depth + 1 // B 方弱 1 层
		}
		mv, ok := game.FindBestMoveAtDepth(state.Board, player, curDepth)

		if !ok {
			break
		}
		tensor := game.EncodeBoardTensor(state.Board, player)
		mvIdx := game.AxialToIndex(mv.To)
		row := make([]string, 0, game.TensorLen+3)
		for _, v := range tensor {
			if v == 0 {
				row = append(row, "0")
			} else {
				row = append(row, "1")
			}
		}
		row = append(row, strconv.Itoa(mvIdx)) // 243
		rows = append(rows, row)               // z 先留空
		state.MakeMove(mv)
		player = game.Opponent(player)
		moves++
		if moves >= maxMoves {
			return nil, false
		}
		if state.GameOver {
			break
		}
	}
	if moves < minMoves {
		return nil, false
	}

	z := winnerValue(state)
	gameID := strconv.Itoa(id)
	for i := range rows {
		rows[i] = append(rows[i], strconv.Itoa(z), gameID)
	}

	return rows, true
}

func addRandomOpening(st *game.GameState, n int, r *rand.Rand) {
	for i := 0; i < n; i++ {
		for _, pl := range []game.CellState{game.PlayerA, game.PlayerB} {
			moves := game.GenerateMoves(st.Board, pl)
			if len(moves) == 0 {
				continue
			}
			mv := moves[r.Intn(len(moves))] // 专属随机源
			st.MakeMove(mv)
		}
	}
}

// winnerValue 根据最终棋子数返回 +1 / 0 / -1
func winnerValue(st *game.GameState) int {
	a := st.Board.CountPieces(game.PlayerA)
	b := st.Board.CountPieces(game.PlayerB)
	if a > b {
		return 1
	}
	if b > a {
		return -1
	}
	return 0
}

// repairCSV 打开文件检查尾行完整性，返回已有完整局数
func repairCSV(path string, expectCols int) int {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Fatalf("repair open: %v", err)
	}
	defer f.Close()

	var offset int64
	rdr := bufio.NewReader(f)
	cols := 0
	lines := 0
	for {
		line, err := rdr.ReadBytes('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			log.Fatalf("read csv: %v", err)
		}
		cols = countCSVColumns(line)
		if cols == expectCols {
			offset += int64(len(line))
			lines++
		} else {
			break // 遇到半行
		}
	}
	if cols != 0 && cols != expectCols {
		// 截断到最后完整行末尾
		if err := f.Truncate(offset); err != nil {
			log.Fatalf("truncate: %v", err)
		}
		log.Printf("检测到残缺行，已截断到 %d 字节 (完整 %d 行)", offset, lines)
	}
	return lines // 一行=一步，不是一局。要精确局数可另存标记
}

// 简单统计逗号列数
func countCSVColumns(b []byte) int {
	n := 1
	for _, c := range b {
		if c == ',' {
			n++
		}
	}
	// 去掉末尾 \n
	if len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		// nothing
	}
	return n
}

// go build -ldflags="-s -w" -gcflags="all=-trimpath=${PWD}" -asmflags="all=-trimpath=${PWD}" -o selfplay.exe .\cmd\selfplay\main.go
