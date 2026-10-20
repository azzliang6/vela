package app

import "github.com/egoist/mygo/ui"

// Vela 的纸张色系，取自原 global.css 的 :root 与 [data-theme="dark"]。
func velaTheme(dark bool) *ui.Theme {
	if dark {
		t := ui.DarkTheme()
		t.Background, t.Surface = ui.Hex("#141210"), ui.Hex("#1b1815")
		t.SurfaceHover, t.SurfacePressed = ui.Hex("#25211b"), ui.Hex("#2b2620")
		t.Border, t.Text, t.TextMuted = ui.Hex("#2b2620"), ui.Hex("#eae3d4"), ui.Hex("#8c8272")
		t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#f2611d"), ui.Hex("#ff7a3d"), ui.Hex("#d9480f"), ui.Hex("#1c0d05")
		t.Danger, t.Warning, t.Success = ui.Hex("#e06c6c"), ui.Hex("#e0a24c"), ui.Hex("#6ab98d")
		t.Selection, t.Focus = ui.RGBA(242, 97, 29, 0.28), ui.RGBA(242, 97, 29, 0.6)
		t.Radius = 8
		return t
	}
	t := ui.LightTheme()
	t.Background, t.Surface = ui.Hex("#f7f4ea"), ui.Hex("#fdfbf4")
	t.SurfaceHover, t.SurfacePressed = ui.Hex("#efe9da"), ui.Hex("#e5dfcd")
	t.Border, t.Text, t.TextMuted = ui.Hex("#e5dfcd"), ui.Hex("#1d1a15"), ui.Hex("#867d6d")
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#d9480f"), ui.Hex("#c2410c"), ui.Hex("#b23a08"), ui.Hex("#fff6ee")
	t.Danger, t.Warning, t.Success = ui.Hex("#b83232"), ui.Hex("#d97706"), ui.Hex("#2f7a4e")
	t.Selection, t.Focus = ui.RGBA(217, 72, 15, 0.18), ui.RGBA(217, 72, 15, 0.55)
	t.Radius = 8
	return t
}

var starColor = map[bool]ui.Color{false: ui.Hex("#ffaa00"), true: ui.Hex("#ffc53d")}
