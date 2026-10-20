package app

import (
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// VELA_SHOTS=dir go test -run TestShots 会把主要界面渲染成 PNG，用于人工检查。
func TestShots(t *testing.T) {
	dir := os.Getenv("VELA_SHOTS")
	if dir == "" {
		t.Skip("设置 VELA_SHOTS 才渲染截图")
	}
	save := func(tt *ui.Tester, name string) {
		f, _ := os.Create(dir + "/" + name + ".png")
		defer f.Close()
		_ = png.Encode(f, tt.Image())
	}
	a, _ := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("信用卡账单")
	save(tt, "main")
	_ = tt.Click("回复")
	save(tt, "compose")
	a.composeOpen = false
	tt.Frame()
	_ = tt.Click("添加邮箱")
	save(tt, "add")
	a.addOpen = false
	a.settings.Theme = "dark"
	tt.Frame()
	save(tt, "dark")
}
