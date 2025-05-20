// internal/assets/animation.go
package assets

import (
	"bytes"
	"embed"
	"image/png"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

//embed 所有 PNG

//go:embed animation/**.png
var animFS embed.FS

// AnimFrames["redJump/down"] = []*ebiten.Image{frame0, frame1, …}
var AnimFrames = map[string][]*ebiten.Image{}

// 在 init 一次性加载
func init() {
	loadDir("animation")
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
