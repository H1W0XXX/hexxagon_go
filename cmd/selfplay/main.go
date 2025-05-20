// cmd/selfplay/main.go
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	//"strconv"
	"time"

	"hexxagon_go/internal/game"
)

const (
	CLONE_BONUS_MAX = 40 // 开局分裂强引导
	CLONE_BONUS_MIN = 14 // 终局回到 1 子价值
)

var writeMu sync.Mutex

var (
	outFile   = flag.String("o", "train.csv", "输出 CSV 文件")
	episodes  = flag.Int("games", 100000, "自对弈局数")
	maxPlies  = flag.Int("maxplies", 400, "单局最大步数")
	radius    = flag.Int("radius", 3, "棋盘半径")
	epsilon   = flag.Float64("eps", 0.10, "ε-greedy 随机率")
	deepDepth = flag.Int("deeplvl", 6, "标签搜索深度")
	labelRate = flag.Float64("labelRate", 0.3, "深度评估标签比例")
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()
	rand.Seed(time.Now().UnixNano())

	f, err := os.Create(*outFile)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	signalC := make(chan os.Signal, 1)
	signal.Notify(signalC, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalC
		log.Println("收到 Ctrl-C，正在刷写缓冲区 …")
		w.Flush()
		os.Exit(1)
	}()

	writeHeader(w, *radius)

	jobs := make(chan int, runtime.NumCPU()*2)
	var wg sync.WaitGroup

	const maxRetry = 3

	// 启动固定数量的 worker
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				success := false
				for attempt := 1; attempt <= maxRetry; attempt++ {
					func() {
						defer func() {
							if r := recover(); r != nil {
								log.Printf("genOneGame(%d) panic on attempt %d: %v", idx, attempt, r)
							}
						}()
						// 这行真正跑一局，包含 pickMove 调用
						genOneGame(w)
						success = true // 如果没 panic，就算成功
					}()
					if success {
						break
					}
				}
				if !success {
					log.Printf("genOneGame(%d) failed after %d attempts, skipping", idx, maxRetry)
				}
				// 日志进度
				if (idx+1)%1000 == 0 {
					writeMu.Lock()
					log.Printf("完成 %d / %d 局\n", idx+1, *episodes)
					writeMu.Unlock()
				}
			}
		}()
	}

	// 投递任务
	for g := 0; g < *episodes; g++ {
		jobs <- g
	}
	close(jobs)

	wg.Wait()
	w.Flush()
}

// ───────────────────────────────────────────────────────────
// 自对弈
// ───────────────────────────────────────────────────────────
type stateSnap struct {
	board *game.Board
	side  game.CellState
	mv    game.Move
}

func genOneGame(w *bufio.Writer) int {
	b := game.NewBoard(*radius)
	randomOpening(b)
	side := game.PlayerA

	// 记录每一步的 (局面, 轮到谁, 本步走子)
	var hist []stateSnap
	for ply := 0; ply < *maxPlies; ply++ {
		// 先克隆当前局面
		before := b.Clone()
		// 决定本步走法
		mv := pickMove(b, side)
		hist = append(hist, stateSnap{board: before, side: side, mv: mv})

		if mv == nilMove {
			break
		}
		_, _ = mv.MakeMove(b, side)

		if len(game.GenerateMoves(b, game.Opponent(side))) == 0 {
			side = game.Opponent(side)
			break
		}
		side = game.Opponent(side)
	}

	winner := winnerByPieces(b)

	// 3) 写标签时，前 K 步硬性分裂奖励
	const K = 4
	wrote := 0
	for i, s := range hist {
		var lbl int

		// 前 K 步：如果是分裂给个正标签，否则给个负标签
		if i < K {
			if s.mv.IsClone() {
				lbl = 16000 // 比如半胜值
			} else {
				lbl = -16000
			}

		} else {
			// 原来的深度 or 终局标签逻辑
			deep := (i%3 == 0) && rand.Float64() < *labelRate
			if deep {
				lbl = deepEvalFast(s.board, s.side, *deepDepth)
			}
			if i == len(hist)-1 {
				switch winner {
				case game.PlayerA:
					lbl = 32000
				case game.PlayerB:
					lbl = -32000
				}
			}
		}

		writeRow(w, s.board, s.side, lbl)
		wrote++
	}
	return wrote
}

func cloneBonus(b *game.Board) int {
	empties := 0
	for _, c := range b.AllCoords() {
		if b.Get(c) == game.Empty {
			empties++
		}
	}
	r := float64(empties) / float64(len(b.AllCoords())) // 1 开局 → 0 终局
	bonus := r*CLONE_BONUS_MAX + (1-r)*CLONE_BONUS_MIN
	return int(bonus + 0.5) // 四舍五入
}

// ε-greedy 选招：90 % bestMove，10 % random
func pickMove(b *game.Board, side game.CellState) game.Move {

	moves := game.GenerateMoves(b, side)
	if len(moves) == 0 {
		return nilMove
	}
	if rand.Float64() < *epsilon {
		return moves[rand.Intn(len(moves))]
	}

	bonus := cloneBonus(b) // ← 每步实时计算奖励
	bestScore := math.MinInt32
	var best game.Move

	for _, m := range moves {
		nb := b.Clone()
		_, _ = m.MakeMove(nb, side)

		sc := game.Evaluate(nb, side)
		if m.IsClone() {
			sc += bonus // 动态加成
		}
		if sc > bestScore {
			bestScore, best = sc, m
		}
	}
	return best
}

// 深层搜索分数作为标签
func deepEval(b *game.Board, side game.CellState) int {
	score := game.DeepSearch(b.Clone(), b.Hash(), side, *deepDepth) // 你现有 α-β 函数改个包装
	// 归一到 –32000…+32000 区间
	if score > 32000 {
		score = 32000
	}
	if score < -32000 {
		score = -32000
	}
	return score
}

func deepEvalFast(b *game.Board, side game.CellState, depth int) int {
	// 直接用当前指针，回溯靠 Make/Unmake
	return game.DeepSearch(b, b.Hash(), side, depth)
}

// ───────────────────────────────────────────────────────────
// 工具函数
// ───────────────────────────────────────────────────────────

func writeHeader(w *bufio.Writer, r int) {
	N := r*3*(r+1) + 1
	for i := 0; i < N; i++ {
		fmt.Fprintf(w, "c%d,", i)
	}
	fmt.Fprintln(w, "stm,eval")
}

func writeRow(w *bufio.Writer, b *game.Board, side game.CellState, label int) {
	writeMu.Lock()
	defer writeMu.Unlock()
	for _, c := range b.AllCoords() {
		v := 0
		switch b.Get(c) {
		case game.PlayerA:
			v = 1
		case game.PlayerB:
			v = -1
		}
		fmt.Fprintf(w, "%d,", v)
	}
	stm := 1
	if side == game.PlayerB {
		stm = -1
	}
	fmt.Fprintf(w, "%d,%d\n", stm, label)
}

func randomOpening(b *game.Board) {
	// 简易随机铺 2+2 子
	for placed := 0; placed < 2; {
		c := b.AllCoords()[rand.Intn(len(b.AllCoords()))]
		if b.Get(c) == game.Empty {
			b.Set(c, game.PlayerA)
			placed++
		}
	}
	for placed := 0; placed < 2; {
		c := b.AllCoords()[rand.Intn(len(b.AllCoords()))]
		if b.Get(c) == game.Empty {
			b.Set(c, game.PlayerB)
			placed++
		}
	}
}

func winnerByPieces(b *game.Board) game.CellState {
	a := b.CountPieces(game.PlayerA)
	ba := b.CountPieces(game.PlayerB)
	switch {
	case a > ba:
		return game.PlayerA
	case ba > a:
		return game.PlayerB
	default:
		return 0 // 平
	}
}

var nilMove game.Move
