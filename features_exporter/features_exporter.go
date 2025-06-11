// features_exporter.go
package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Move represents a single move in a game step.
type Move struct {
	From struct{ Q, R int }
	To   struct{ Q, R int }
}

// Step represents one turn: the board state and the move taken.
type Step struct {
	Board map[string]int
	Move  Move
}

// Game represents a full self‐play game.
type Game struct {
	Winner string
	Steps  []Step
}

const radius = 4

var directions = [][2]int{
	{1, 0}, {1, -1}, {0, -1},
	{-1, 0}, {-1, 1}, {0, 1},
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func parseKey(key string) (q, r int) {
	// strip parentheses
	s := key[1 : len(key)-1]
	// find comma
	i := strings.IndexByte(s, ',')
	// Atoi is faster than Sscanf here
	q, _ = strconv.Atoi(s[:i])
	r, _ = strconv.Atoi(s[i+1:])
	return
}

func extractFeatures(
	board map[[2]int]int,
	coords [][2]int,
	last *Move,
) []float64 {

	// helper to get cell state
	get := func(q, r int) int {
		return board[[2]int{q, r}]
	}
	// opp
	opp := func(pl int) int {
		if pl == 2 {
			return 1
		}
		return 2
	}
	// is outer
	isOuter := func(q, r int) bool {
		return max3(absInt(q), absInt(r), absInt(q+r)) == radius
	}
	// in opponent range
	inOppRange := func(q, r, opl int) bool {
		for _, d := range directions {
			nq, nr := q+d[0], r+d[1]
			if get(nq, nr) == opl {
				return true
			}
			// 检查二步斜跳
			for _, d2 := range directions {
				if absInt(d2[0])+absInt(d2[1]) == 2 {
					q2, r2 := nq+d2[0], nr+d2[1]
					if get(q2, r2) == opl {
						return true
					}
				}
			}
		}
		return false
	}
	// preview_infected
	previewInfected := func(qf, rf, qt, rt, pl int) int {
		cnt := 0
		for _, d := range directions {
			if get(qt+d[0], rt+d[1]) == opp(pl) {
				cnt++
			}
		}
		dist := max3(
			absInt(qf-qt),
			absInt(rf-rt),
			absInt((qf+rf)-(qt+rt)),
		)
		weight := 2
		if dist == 2 {
			weight = 1
		}
		return cnt * weight
	}
	// collect all coords

	empties := 0
	for _, coord := range coords {
		// coord 是 [2]int{q, r]
		if board[coord] == 0 {
			empties++
		}
	}
	n := len(coords)
	rEmp := float64(empties) / float64(n)

	pl := 1
	opl := opp(pl)
	myCnt, opCnt := 0, 0
	outer, danger := 0, 0
	for _, c := range coords {
		q, r := c[0], c[1]
		st := get(q, r)
		if st == pl {
			myCnt++
			if isOuter(q, r) {
				outer++
			}
			if inOppRange(q, r, opl) {
				danger++
			}
		} else if st == opl {
			opCnt++
			if isOuter(q, r) {
				outer--
			}
		}
	}

	// mobility
	cloneMob, fullMob := 0, 0
	for _, c := range coords {
		qf, rf := c[0], c[1]
		if get(qf, rf) != pl {
			continue
		}
		for _, c2 := range coords {
			qt, rt := c2[0], c2[1]
			if get(qt, rt) != 0 {
				continue
			}
			dist := max3(
				absInt(qf-qt),
				absInt(rf-rt),
				absInt((qf+rf)-(qt+rt)),
			)
			if dist == 1 {
				cloneMob++
			} else if dist == 2 {
				fullMob++
			}
		}
	}
	mobDiff := fullMob - cloneMob
	if rEmp >= 0.82 {
		mobDiff = cloneMob
	}

	// infection diff
	bestMy, bestOp := 0, 0
	for _, c := range coords {
		qf, rf := c[0], c[1]
		if get(qf, rf) != pl {
			continue
		}
		for _, c2 := range coords {
			qt, rt := c2[0], c2[1]
			if get(qt, rt) != 0 {
				continue
			}
			val := previewInfected(qf, rf, qt, rt, pl)
			if val > bestMy {
				bestMy = val
			}
		}
	}
	for _, c := range coords {
		qf, rf := c[0], c[1]
		if get(qf, rf) != opl {
			continue
		}
		for _, c2 := range coords {
			qt, rt := c2[0], c2[1]
			if get(qt, rt) != 0 {
				continue
			}
			val := previewInfected(qf, rf, qt, rt, opl)
			if val > bestOp {
				bestOp = val
			}
		}
	}
	infDiffWeighted := bestMy - bestOp

	// —— 5) 空洞惩罚 —— //
	// 限制在棋盘内的空格
	posSet := make(map[[2]int]bool, len(coords))
	for _, c := range coords {
		posSet[c] = true
	}
	visited := make(map[[2]int]bool)
	holeWeight := 5 // 空洞区域被对手跳入即惩罚，可调
	holesPenalty := 0
	for _, start := range coords {
		if board[start] != 0 || visited[start] {
			continue
		}
		// BFS 收集连通空洞区域
		queue := [][2]int{start}
		region := [][2]int{start}
		visited[start] = true
		for len(queue) > 0 {
			cell := queue[0]
			queue = queue[1:]
			for _, d := range directions {
				nb := [2]int{cell[0] + d[0], cell[1] + d[1]}
				if !posSet[nb] || visited[nb] || board[nb] != 0 {
					continue
				}
				visited[nb] = true
				queue = append(queue, nb)
				region = append(region, nb)
			}
		}
		// 收集对手位置
		var oppPositions [][2]int
		for _, c := range coords {
			if board[c] == opl {
				oppPositions = append(oppPositions, c)
			}
		}
		// 判断对手能否1~2步进入
		opponentCanReach := false
		for _, cell := range region {
			if opponentCanReach {
				break
			}
			for _, oppCell := range oppPositions {
				d := max3(
					absInt(oppCell[0]-cell[0]),
					absInt(oppCell[1]-cell[1]),
					absInt((oppCell[0]+oppCell[1])-(cell[0]+cell[1])),
				)
				if d <= 2 {
					opponentCanReach = true
					break
				}
			}
		}
		if opponentCanReach {
			holesPenalty += len(region) * holeWeight
		}
	}

	openingPenalty := 0
	if rEmp >= 0.82 && opCnt > myCnt {
		openingPenalty = (opCnt - myCnt) * 10
	}
	earlyJumpCost := 0
	if rEmp >= 0.82 && cloneMob > 0 && infDiffWeighted == 0 {
		earlyJumpCost = 20
	}

	lastMoveBonus := 0
	isJump := 0.0
	if last != nil {
		fq, fr := last.From.Q, last.From.R
		tq, tr := last.To.Q, last.To.R
		dist := max3(
			absInt(fq-tq),
			absInt(fr-tr),
			absInt((fq+fr)-(tq+tr)),
		)
		if dist == 2 {
			isJump = 1.0
		}
		if rEmp < 0.6 {
			for _, d := range directions {
				if get(tq+d[0], tr+d[1]) == 0 {
					lastMoveBonus += 15
				}
			}
		}
	}

	return []float64{
		rEmp,
		float64(myCnt - opCnt),
		float64(outer),
		float64(danger),
		float64(mobDiff),
		float64(infDiffWeighted),
		float64(holesPenalty),
		float64(openingPenalty),
		float64(earlyJumpCost),
		float64(lastMoveBonus),
		isJump,
	}
}

// max3 returns the maximum of three ints.
func max3(a, b, c int) int {
	return int(math.Max(float64(a), math.Max(float64(b), float64(c))))
}

func processFile(fn string, out chan<- []string, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "panic processing %s: %v\n", fn, r)
		}
	}()
	//fmt.Println("processing", fn)

	data, err := os.ReadFile(fn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s error: %v\n", fn, err)
		return
	}
	if len(data) == 0 {
		fmt.Fprintf(os.Stderr, "skip empty file %s\n", fn)
		return
	}
	if !json.Valid(data) {
		fmt.Fprintf(os.Stderr, "skip invalid JSON %s\n", fn)
		return
	}

	var g Game
	if err := json.Unmarshal(data, &g); err != nil {
		fmt.Fprintf(os.Stderr, "unmarshal %s error: %v\n", fn, err)
		return
	}

	label := "1"
	if g.Winner != "A" {
		label = "-1"
	}
	for i, step := range g.Steps {
		// —— ① 把 map[string]int 先转一次
		intBoard := make(map[[2]int]int, len(step.Board))
		coords := make([][2]int, 0, len(step.Board))
		for strKey, st := range step.Board {
			// parseKey("(q,r)") 返回两个 int
			q, r := parseKey(strKey)
			pos := [2]int{q, r}
			intBoard[pos] = st
			coords = append(coords, pos)
		}

		// 如果真的没有格子，直接跳过，避免除 0
		if len(coords) == 0 {
			continue
		}

		// —— ② 上一步 move
		var last *Move
		if i > 0 {
			last = &g.Steps[i-1].Move
		}

		// —— ③ 用新的签名调用
		feats := extractFeatures(intBoard, coords, last)

		// —— ④ 写 CSV
		row := make([]string, len(feats)+1)
		for j, v := range feats {
			row[j] = fmt.Sprintf("%f", v)
		}
		row[len(feats)] = label
		out <- row
	}
}
func main() {
	files, err := filepath.Glob("selfplayData/*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "glob error: %v\n", err)
		os.Exit(1)
	}

	outFile, err := os.OpenFile("features.csv",
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create output file error: %v\n", err)
		os.Exit(1)
	}
	defer outFile.Close()

	bw := bufio.NewWriter(outFile)
	writer := csv.NewWriter(bw)
	defer writer.Flush()
	defer bw.Flush() // 一定要把 bufio 的缓冲也 flush 到磁盘

	runtime.GOMAXPROCS(runtime.NumCPU())
	var wg sync.WaitGroup
	rows := make(chan []string, 1000)

	// 写 CSV 的 goroutine
	go func() {
		for row := range rows {
			if err := writer.Write(row); err != nil {
				fmt.Fprintf(os.Stderr, "write row error: %v\n", err)
			}
		}
	}()

	// 信号量：最多 runtime.NumCPU() 个并发 worker
	sem := make(chan struct{}, runtime.NumCPU())

	for _, fn := range files {
		wg.Add(1)
		sem <- struct{}{} // acquire
		go func(fn string) {
			defer func() { <-sem }() // release
			processFile(fn, rows, &wg)
		}(fn)
	}

	wg.Wait()
	close(rows)
	fmt.Println("Done exporting features.")
}

// go build -ldflags="-s -w" -gcflags="all=-trimpath=${PWD}" -asmflags="all=-trimpath=${PWD}" -o features_exporter.exe .\features_exporter\features_exporter.go
