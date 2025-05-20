// internal/game/tt.go
package game

import (
	"math/rand"
	"sync"
)

// ---------- Zobrist 随机键 ----------
var zKey = struct {
	mu   sync.RWMutex
	cell map[HexCoord][4]uint64
}{
	cell: make(map[HexCoord][4]uint64, 256),
}

func zobristKey(c HexCoord, s CellState) uint64 {
	// 先无锁读
	zKey.mu.RLock()
	k, ok := zKey.cell[c]
	zKey.mu.RUnlock()
	if ok {
		return k[s]
	}

	// 双检：不存在时才加写锁生成
	zKey.mu.Lock()
	// 可能别的线程刚写完，再检查一次
	if k, ok = zKey.cell[c]; !ok {
		k = [4]uint64{rand.Uint64(), rand.Uint64(), rand.Uint64(), rand.Uint64()}
		zKey.cell[c] = k
	}
	zKey.mu.Unlock()
	return k[s]
}

// 计算整盘哈希；为简单 CPU→内存换，直接整盘 XOR。
// 后续想再抠性能，可做“增量哈希”(Make/Unmake)。
func hashBoard(b *Board) uint64 {
	var h uint64
	for coord, state := range b.cells {
		if state != Empty {
			h ^= zobristKey(coord, state)
		}
	}
	return h
}

// ---------- 置换表 ----------

const ttSize = 1 << 22 // 4 M entry (≈96 MB)
const ttMask = ttSize - 1

type ttFlag uint8

const (
	ttExact ttFlag = iota
	ttLower
	ttUpper
)

type ttEntry struct {
	key     uint64 // 高 64 bit 哈希
	score   int32  // αβ 得分
	depth   int16  // 搜索深度
	flag    ttFlag // 界类型
	bestIdx uint8
}

// 直接用切片代替 map：速度更高，占内存可控。
var ttTable = make([]ttEntry, ttSize)

var ttMu [256]sync.Mutex // 分片锁：低开销，冲突少

func lockFor(hash uint64) *sync.Mutex { return &ttMu[hash&255] }

// probeTT 返回 (hit, score, flag)
func probeTT(hash uint64, depth int) (bool, int, ttFlag) {
	e := ttTable[hash&ttMask]
	if e.key == hash && int(e.depth) >= depth {
		return true, int(e.score), e.flag
	}
	return false, 0, 0
}

// storeTT 把新结果写回；若槽已被占，用深度更大的替换（简单策略）
func storeTT(hash uint64, depth, score int, flag ttFlag) {
	idx := hash & ttMask
	if int(ttTable[idx].depth) <= depth {
		ttTable[idx] = ttEntry{
			key:   hash,
			score: int32(score),
			depth: int16(depth),
			flag:  flag,
		}
	}
}

func probeBestIdx(hash uint64) (bool, uint8) {
	e := ttTable[hash&ttMask]
	if e.key == hash {
		return true, e.bestIdx
	}
	return false, 0
}

func storeBestIdx(hash uint64, idx uint8) {
	e := &ttTable[hash&ttMask]
	if e.key == hash { // 仅限同一哈希
		e.bestIdx = idx // 覆盖即可
	}
}
