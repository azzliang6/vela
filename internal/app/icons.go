package app

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

//go:embed icons/*.svg
var iconFS embed.FS

var iconCache = map[string]*ui.SVG{}

// icon 返回 Lucide 图标（ISC 许可，与原 React 版 lucide-react 同一套）。
func icon(name string) *ui.SVG {
	if s, ok := iconCache[name]; ok {
		return s
	}
	b, err := iconFS.ReadFile("icons/" + name + ".svg")
	if err != nil {
		return nil
	}
	s := ui.MustParseSVG(b)
	iconCache[name] = s
	return s
}
