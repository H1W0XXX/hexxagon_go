// internal/assets/animation.go
package assets

import (
	"bytes"
	"embed"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

//embed 所有 PNG

//go:embed animation
var animFS embed.FS

type AnimData struct {
	Frames []*ebiten.Image
	AX, AY float64 // anchor (像素)
	FPS    float64
}

var AnimDatas = map[string]AnimData{}
var AnimFrames map[string][]*ebiten.Image

func init() {
	AnimFrames = make(map[string][]*ebiten.Image)

	baseDir := "internal/assets/animation"

	// WalkDir 第一次只遍历目录，不递归文件
	filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == baseDir {
			return err
		}

		// path 形如 ".../redJump/down"
		rel, _ := filepath.Rel(baseDir, path) // "redJump/down"
		rel = filepath.ToSlash(rel)           // 统一斜杠

		// 读取该目录下所有 .png，并排序
		files, _ := filepath.Glob(filepath.Join(path, "*.png"))
		sort.Strings(files)

		// ←───── 新增：为本目录准备帧切片与锚点 ──────→
		var frames []*ebiten.Image
		var ax, ay float64

		for i, f := range files {
			// ① 读原始 PNG
			pngFile, _ := os.Open(f)
			srcImg, _, _ := image.Decode(pngFile)
			pngFile.Close()

			// ② 转 ebiten.Image，收集帧
			frames = append(frames, ebiten.NewImageFromImage(srcImg))

			// ③ 仅用首帧计算锚点
			if i == 0 {
				ax, ay = autoAnchor(srcImg)
			}
		}

		// —— 把结果写入全局表 —— //
		if len(frames) > 0 {
			AnimFrames[rel] = frames   // 旧用法保留
			AnimDatas[rel] = AnimData{ // 新增锚点
				Frames: frames,
				AX:     ax,
				AY:     ay,
				FPS:    30, // 如有需要可改
			}
		}
		return nil
	})
}

func loadEbitenImage(path string) *ebiten.Image {
	imgFile, _ := os.Open(path)
	defer imgFile.Close()
	img, _, _ := image.Decode(imgFile)
	return ebiten.NewImageFromImage(img)
}

// 递归遍历文件夹
func loadDir(path string) {
	entries, _ := fs.ReadDir(animFS, path)
	if len(entries) == 0 {
		return
	}

	// 收集当前目录下的 PNG
	var pngFiles []string
	for _, e := range entries {
		if e.IsDir() {
			loadDir(filepath.Join(path, e.Name())) // 继续递归
		} else if strings.HasSuffix(e.Name(), ".png") {
			pngFiles = append(pngFiles, filepath.Join(path, e.Name()))
		}
	}

	if len(pngFiles) == 0 {
		return
	}

	sort.Strings(pngFiles) // 001,002,… 顺序
	key := strings.TrimPrefix(path, "animation/")
	for _, fp := range pngFiles {
		data, _ := animFS.ReadFile(fp)
		img, _ := png.Decode(bytes.NewReader(data))
		AnimFrames[key] = append(AnimFrames[key], ebiten.NewImageFromImage(img))
	}
}

// autoAnchor 取首帧非透明像素包络框中心；全透明就返回图片中心
func autoAnchor(img image.Image) (float64, float64) {
	minX, minY := img.Bounds().Max.X, img.Bounds().Max.Y
	maxX, maxY := 0, 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	// 全透明 ⇒ 取整图中心
	if minX > maxX || minY > maxY {
		return float64(img.Bounds().Dx()) / 2, float64(img.Bounds().Dy()) / 2
	}
	return float64(minX+maxX) / 2, float64(minY+maxY) / 2
}
