// game/ai.go
package game

import (
	"math"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
	runtime.GOMAXPROCS(runtime.NumCPU()) // 吃满物理/逻辑核心
	rand.Seed(time.Now().UnixNano())
}

// ------------------------------------------------------------
// 公共入口
// ------------------------------------------------------------
func FindBestMove(b *Board, player CellState) (Move, bool) {
	moves := GenerateMoves(b, player)
	if len(moves) == 0 {
		return Move{}, false
	}

	const depth = 4
	const inf = 1 << 30

	// ---------- 1) 走法粗评分 ----------
	type scored struct {
		mv    Move
		score int
	}
	order := make([]scored, 0, len(moves))
	for _, m := range moves {
		cnt, _ := m.ApplyPreview(b, player) // 仍可用旧的预估
		gain := cnt
		if m.IsClone() {
			gain++
		}
		order = append(order, scored{m, gain})
	}
	sort.Slice(order, func(i, j int) bool { return order[i].score > order[j].score })

	// ---------- 2) 并行根节点 ----------
	type result struct {
		mv    Move
		score int
	}
	resCh := make(chan result, len(order))
	var wg sync.WaitGroup

	alphaRoot := -inf
	betaRoot := inf

	for _, item := range order {
		wg.Add(1)
		go func(it scored) {
			defer wg.Done()

			nb := b.Clone()                   // 根节点仍做一次深拷贝
			_, _ = it.mv.MakeMove(nb, player) // ⭐ 用 MakeMove 代替 Apply
			// MakeMove 已自动更新 nb.hash

			score := alphaBeta(nb, nb.hash,
				Opponent(player), player, depth-1, alphaRoot, betaRoot)

			resCh <- result{it.mv, score}
		}(item)
	}
	wg.Wait()
	close(resCh)

	// ---------- 3) 汇总最佳 ----------
	bestScore := -inf
	var bestMoves []Move
	for r := range resCh {
		switch {
		case r.score > bestScore:
			bestScore, bestMoves = r.score, []Move{r.mv}
		case r.score == bestScore:
			bestMoves = append(bestMoves, r.mv)
		}
	}
	choice := bestMoves[rand.Intn(len(bestMoves))]
	return choice, true
}

// ------------------------------------------------------------
// α-β + 置换表
// ------------------------------------------------------------
func alphaBeta(
	b *Board,
	hash uint64,
	current, original CellState,
	depth, alpha, beta int,
) int {
	if depth == 0 {
		return evaluate(b, original)
	}

	// ---------- 置换表探测 ----------
	if hit, val, flag := probeTT(hash, depth); hit {
		switch flag {
		case ttExact:
			return val
		case ttLower:
			if val > alpha {
				alpha = val
			}
		case ttUpper:
			if val < beta {
				beta = val
			}
		}
		if alpha >= beta {
			return val
		}
	}
	alphaOrig := alpha // 用于写表判定

	moves := GenerateMoves(b, current)
	if len(moves) == 0 {
		return evaluate(b, original)
	}

	// 走法排序：感染数降序
	sort.Slice(moves, func(i, j int) bool {
		ci, _ := moves[i].ApplyPreview(b, current)
		cj, _ := moves[j].ApplyPreview(b, current)
		return ci > cj
	})

	best := math.MinInt32
	bestIdx := uint8(0)

	if current == original { // 极大化
		for i, m := range moves {
			nb := b.Clone()
			_, _ = m.Apply(nb, current)
			childHash := hashBoard(nb)

			score := alphaBeta(nb, childHash,
				Opponent(current), original, depth-1, alpha, beta)

			if score > best {
				best = score
				bestIdx = uint8(i)
			}
			alpha = max(alpha, best)
			if beta <= alpha {
				break // β 剪枝
			}
		}
	} else { // 极小化
		best = math.MaxInt32
		for i, m := range moves {
			nb := b.Clone()
			_, _ = m.Apply(nb, current)
			childHash := hashBoard(nb)

			score := alphaBeta(nb, childHash,
				Opponent(current), original, depth-1, alpha, beta)

			if score < best {
				best = score
				bestIdx = uint8(i)
			}
			beta = min(beta, best)
			if beta <= alpha {
				break // α 剪枝
			}
		}
	}

	// ---------- 写回置换表 ----------
	var flag ttFlag
	switch {
	case best <= alphaOrig:
		flag = ttUpper
	case best >= beta:
		flag = ttLower
	default:
		flag = ttExact
	}
	storeTT(hash, depth, best, flag) // 分值
	storeBestIdx(hash, bestIdx)      // 额外存根节点最佳着，函数见 tt.go

	return best
}

// ------------------------------------------------------------
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func DeepSearch(b *Board, hash uint64, side CellState, depth int) int {
	return alphaBeta(b, hash, side, side, depth, -32000, 32000)
}
