// internal/game/onnx_infer.go
package game

import (
	_ "embed"
	//"errors"
	"fmt"
	"os"
	//"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// —— 把 ONNX 模型打进二进制 ——
//
//go:embed assets/hex_cnn.onnx
var onnxBytes []byte

// 如果你的导出脚本用了别的输入/输出名，请改这里：
// 训练脚本里通常是 input_names=["state"], output_names=["policy","value"]
const (
	onnxInputName  = "state"
	onnxPolicyName = "policy"
	onnxValueName  = "value"
	grid           = 9
	radius         = 4
	featPlanes     = 3 // [my, opp, mask]
	policyOutDim   = 81
)

var (
	ortOnce  sync.Once
	ortErr   error
	ortSess  *ort.AdvancedSession
	ortMu    sync.Mutex // AdvancedSession 里绑定了固定的张量，这里串行化 Run，先稳妥跑通
	inTensor *ort.Tensor[float32]
	outP     *ort.Tensor[float32]
	outV     *ort.Tensor[float32]
	tmpModel string
)

// Lazy 初始化 ONNX Runtime & 会话
func ensureONNX() error {
	ortOnce.Do(func() {
		// 1) 选择共享库路径（可删掉，改用环境变量 ONNXRUNTIME_SHARED_LIBRARY_PATH）
		if p := os.Getenv("ONNXRUNTIME_SHARED_LIBRARY_PATH"); p != "" {
			ort.SetSharedLibraryPath(p) // 官方建议明确设置路径，避免默认名找不到库
		}
		if err := ort.InitializeEnvironment(); err != nil {
			ortErr = fmt.Errorf("InitializeEnvironment: %w", err)
			return
		}
		// 退出时不强制销毁，进程结束会释放；若你想显式释放，可暴露 ShutdownONNX()
		// defer ort.DestroyEnvironment()

		// 2) 将内嵌模型写到临时文件
		f, err := os.CreateTemp("", "hex_cnn_*.onnx")
		if err != nil {
			ortErr = err
			return
		}
		tmpModel = f.Name()
		if _, err := f.Write(onnxBytes); err != nil {
			_ = f.Close()
			ortErr = err
			return
		}
		_ = f.Close()

		// 3) 准备 I/O 张量，并用 AdvancedSession 绑定（固定 I/O，Run 时复用缓冲）
		var e error
		inTensor, e = ort.NewTensor(ort.NewShape(1, featPlanes, grid, grid), make([]float32, featPlanes*grid*grid))
		if e != nil {
			ortErr = e
			return
		}
		outP, e = ort.NewEmptyTensor[float32](ort.NewShape(1, policyOutDim))
		if e != nil {
			ortErr = e
			return
		}
		outV, e = ort.NewEmptyTensor[float32](ort.NewShape(1, 1))
		if e != nil {
			ortErr = e
			return
		}

		ortSess, e = ort.NewAdvancedSession(tmpModel,
			[]string{onnxInputName}, []string{onnxPolicyName, onnxValueName},
			[]ort.Value{inTensor}, []ort.Value{outP, outV}, nil)
		if e != nil {
			ortErr = e
			return
		}
	})
	return ortErr
}

// 可选：在程序退出时调用，清理临时文件与环境
func ShutdownONNX() {
	if tmpModel != "" {
		_ = os.Remove(tmpModel)
	}
	if ortSess != nil {
		ortSess.Destroy()
		ortSess = nil
	}
	ort.DestroyEnvironment()
}

// 计算 (q,r) 是否在半径为 4 的六边形棋盘内
func inBounds(q, r int) bool {
	return abs(q) <= radius && abs(r) <= radius && abs(-q-r) <= radius
}
func toIndex(q, r int) int { // 9x9 平面索引
	return (r+radius)*grid + (q + radius)
}

// 把 Board 编成 3×9×9：my=1 / opp=1 / mask=1
func encodeBoard(b *Board, me CellState, dst []float32) {
	for i := range dst {
		dst[i] = 0
	}
	// plane offsets
	offMy, offOpp, offMask := 0, grid*grid, 2*grid*grid
	for r := -radius; r <= radius; r++ {
		for q := -radius; q <= radius; q++ {
			idx := toIndex(q, r)
			if !inBounds(q, r) {
				continue
			}
			switch b.Get(HexCoord{Q: q, R: r}) {
			case me:
				dst[offMy+idx] = 1
			case Opponent(me):
				dst[offOpp+idx] = 1
			}
			dst[offMask+idx] = 1
		}
	}
}

// 只取 value 头做静态评估（返回 int，方便接到你的评分框架）
func EvaluateNN(b *Board, me CellState) int {
	if err := ensureONNX(); err != nil {
		// 回退到旧静态评估也行：
		// return evaluateStatic(b, me)
		return 0
	}
	// 填充输入
	data := inTensor.GetData()
	encodeBoard(b, me, data)

	// 跑一次
	ortMu.Lock()
	err := ortSess.Run()
	ortMu.Unlock()
	if err != nil {
		return 0
	}
	// 读取 value，范围(-1,1)，放大到可比较的整数
	v := outV.GetData()[0]
	return int(v * 100.0)
}

// 可选：拿策略头（81 logits，自己在 Go 侧做 mask/softmax/挑选）
func PolicyNN(b *Board, me CellState) ([]float32, error) {
	if err := ensureONNX(); err != nil {
		return nil, err
	}
	// 输入
	data := inTensor.GetData()
	encodeBoard(b, me, data)

	ortMu.Lock()
	err := ortSess.Run()
	ortMu.Unlock()
	if err != nil {
		return nil, err
	}
	logits := make([]float32, policyOutDim)
	copy(logits, outP.GetData())
	// 这里不做 softmax；若需要概率，再减去 max 然后做 exp/sum
	return logits, nil
}

// —— 小工具 ——
// 直接给 policy 向量打非法格 -Inf
func MaskPolicyInPlace(p []float32) {
	const negInf = -1.0e30
	i := 0
	for r := -radius; r <= radius; r++ {
		for q := -radius; q <= radius; q++ {
			if !inBounds(q, r) {
				p[i] = negInf
			}
			i++
		}
	}
}
