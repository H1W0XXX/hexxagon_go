// internal/game/policy_prune.go
package game

import "sort"

// 开关 & 策略参数（可以按需微调）
var (
	// 总开关：只要 true，就在根节点用 CNN policy 先验修剪
	policyPruneEnabled = true

	// 保留比例 + 下限/上限：keep = clamp(minKeep, int(len(moves)*keepRatio), maxKeep)
	policyKeepRatio = 0.65
	policyMinKeep   = 8
	policyMaxKeep   = 32

	// 如果想把保留下来的走法顺序也按 policy 排序供后续 α–β 使用
	policyAlsoOrder = true
)

// 9x9 平面 index （不引入 ml 包，避免 import cycle）
func toIndex9(b *Board, c HexCoord) int {
	grid := 2*b.radius + 1 // radius=4 -> grid=9
	return (c.R+b.radius)*grid + (c.Q + b.radius)
}

// 根节点：用 CNN policy 砍掉尾部走法
func policyPruneRoot(b *Board, player CellState, moves []Move) []Move {
	if !policyPruneEnabled || len(moves) <= policyMinKeep {
		return moves
	}

	logits, err := PolicyNN(b, player) // []float32, 长度 81
	if err != nil || len(logits) == 0 {
		return moves // 推理失败就不动
	}

	type scored struct {
		mv Move
		p  float32
	}
	arr := make([]scored, 0, len(moves))
	for _, m := range moves {
		idx := toIndex9(b, m.To)
		// 保险：越界/异常就给极小值
		var p float32 = -1e30
		if idx >= 0 && idx < len(logits) {
			p = logits[idx]
		}
		arr = append(arr, scored{m, p})
	}

	// 按先验从大到小排
	sort.Slice(arr, func(i, j int) bool { return arr[i].p > arr[j].p })

	// 计算保留数量
	keep := int(float64(len(arr)) * policyKeepRatio)
	if keep < policyMinKeep {
		keep = policyMinKeep
	}
	if keep > policyMaxKeep {
		keep = policyMaxKeep
	}
	if keep > len(arr) {
		keep = len(arr)
	}

	// 产出
	out := make([]Move, keep)
	for i := 0; i < keep; i++ {
		out[i] = arr[i].mv
	}
	// 如果不想改变后续 α–β 的初始顺序，就把 out 再打乱为原 moves 的相对顺序；
	// 这里多数情况希望“alsoOrder”，即让 α–β 也先搜高先验：
	if policyAlsoOrder {
		return out
	}

	// 只修剪、不改顺序：保留原 moves 顺序
	keepSet := make(map[Move]struct{}, keep)
	for _, m := range out {
		keepSet[m] = struct{}{}
	}
	out2 := out[:0]
	for _, m := range moves {
		if _, ok := keepSet[m]; ok {
			out2 = append(out2, m)
		}
	}
	return out2
}
