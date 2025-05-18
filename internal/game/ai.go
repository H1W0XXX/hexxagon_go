// search.go
package game

import (
	"math"
	"math/rand"
	"sort"
	"time"
)

func init() { rand.Seed(time.Now().UnixNano()) }

// -------- 公开入口 --------
func FindBestMove(b *Board, player CellState) (Move, bool) {
	moves := GenerateMoves(b, player)
	if len(moves) == 0 {
		return Move{}, false
	}

	const depth = 4
	const inf = 1 << 30

	type scored struct {
		mv    Move
		score int
	}
	order := make([]scored, 0, len(moves))

	// ---- 新的粗评分逻辑 ----
	for _, m := range moves {
		cnt, _ := m.ApplyPreview(b, player) // 感染数
		gain := cnt
		if m.IsClone() { // 克隆多 1 子
			gain += 1
		}
		order = append(order, scored{mv: m, score: gain})
	}
	sort.Slice(order, func(i, j int) bool { // 分高的先扩展
		return order[i].score > order[j].score
	})

	bestScore := -inf
	var bestMoves []Move
	alpha, beta := -inf, inf

	for _, item := range order { // 经过排序的迭代
		nb := b.Clone()
		_, _ = item.mv.Apply(nb, player)
		score := alphaBeta(nb, Opponent(player), player, depth-1, alpha, beta)

		switch {
		case score > bestScore:
			bestScore, bestMoves = score, bestMoves[:0]
			bestMoves = append(bestMoves, item.mv)
			alpha = max(alpha, score) // 更新上界
		case score == bestScore:
			bestMoves = append(bestMoves, item.mv)
		}
	}

	// 并列随机
	choice := bestMoves[rand.Intn(len(bestMoves))]
	return choice, true
}

// -------- α-β 剪枝 --------
func alphaBeta(b *Board, current, original CellState, depth, alpha, beta int) int {
	if depth == 0 {
		return evaluate(b, original)
	}
	moves := GenerateMoves(b, current)
	if len(moves) == 0 {
		return evaluate(b, original)
	}

	// 按“潜在感染数”排序，可显著提高剪枝效率
	sort.Slice(moves, func(i, j int) bool {
		ci, _ := moves[i].ApplyPreview(b, current)
		cj, _ := moves[j].ApplyPreview(b, current)
		return ci > cj
	})

	if current == original { // 极大化
		best := math.MinInt32
		for _, m := range moves {
			nb := b.Clone()
			_, _ = m.Apply(nb, current)
			best = max(best, alphaBeta(nb, Opponent(current), original, depth-1, alpha, beta))
			alpha = max(alpha, best)
			if beta <= alpha {
				break
			} // β 剪枝
		}
		return best
	}

	// 极小化
	worst := math.MaxInt32
	for _, m := range moves {
		nb := b.Clone()
		_, _ = m.Apply(nb, current)
		worst = min(worst, alphaBeta(nb, Opponent(current), original, depth-1, alpha, beta))
		beta = min(beta, worst)
		if beta <= alpha {
			break
		} // α 剪枝
	}
	return worst
}

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
