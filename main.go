// Vela 轻帆：现代、美观、快速的桌面邮件客户端。
//
// 架构：纯本地单用户应用，基于 MyGo（纯 Go 桌面框架）。
//   - internal/core：IMAP/SMTP/SQLite/加密/AI 后端（移植自原 Rust 后端）
//   - internal/app：原生界面（GPU 绘制，替代原 React 前端）
//   - HTML 邮件正文用系统 WebView 显示，脚本与跳转被禁止
//
// 关窗只是隐藏到系统托盘（新邮件实时监控在后台继续），真正退出走托盘菜单「退出」。
package main

import (
	_ "embed"
	"log"
	"path/filepath"
	"strings"

	"github.com/azzliang6/vela/internal/app"
	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

//go:embed resources/tray.png
var trayIcon []byte

// webBody 是显示 HTML 正文的 WebView。
type webBody struct {
	wv   *mygo.WebView
	last string
	dark func() bool
}

func (b *webBody) Show(id, html string) {
	if b.wv == nil || b.last == id {
		return
	}
	b.last = id
	b.wv.Page().LoadHTML(app.SanitizeMailHTML(html, b.dark()), "about:blank")
}

func (b *webBody) Native() ui.NativeWebView { return b.wv }

func main() {
	mygo.App.SetName("Vela")
	be, err := core.OpenBackend()
	if err != nil {
		log.Fatal(err)
	}
	a := app.New(be, filepath.Join(core.DataDir(), "settings.json"))

	mygo.App.WhenReady(func() {
		var win *mygo.Window
		var quitting bool
		win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Vela 轻帆", Width: 1280, Height: 820, MinWidth: 1080, MinHeight: 700,
			StateKey: "main", BackgroundColor: "light-dark(#f7f4ea, #141210)",
			Content: ui.View(a.View),
		})
		// 关窗 = 隐藏到托盘，实时监控继续
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				win.Hide()
			}
		})

		body := &webBody{dark: a.Dark}
		if wv, err := win.NewWebView(mygo.WebViewOptions{BackgroundColor: "light-dark(#ffffff, #1b1815)"}); err == nil {
			body.wv = wv
			// 正文里的链接不在内嵌页面里跳转，改用系统浏览器打开
			wv.Page().OnWillNavigate(func(ev *mygo.NavigateEvent) {
				if strings.HasPrefix(ev.URL, "about:") {
					return
				}
				ev.PreventDefault()
				if ev.UserInitiated && (strings.HasPrefix(ev.URL, "http://") || strings.HasPrefix(ev.URL, "https://") || strings.HasPrefix(ev.URL, "mailto:")) {
					_ = mygo.Shell.OpenExternal(ev.URL)
				}
			})
		} else {
			log.Printf("[vela] 无法创建 WebView，HTML 邮件将以纯文本显示：%v", err)
		}

		notify := func(title, text string) {
			if !mygo.NotificationsSupported() {
				return
			}
			go func() { _ = mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: text}).Show() }()
		}
		var bodyView app.BodyView
		if body.wv != nil {
			bodyView = body
		}
		a.SetRuntime(win.Update, notify, bodyView)
		win.Invalidate()

		be.StartWatcher(func(acc string, emails []core.Email) {
			win.Update(func() { a.OnNewMail(acc, emails) })
		})
		win.Update(a.Sync) // 启动后同步一次

		tray, err := mygo.NewTray(mygo.TrayOptions{
			Icon: trayIcon, ToolTip: "Vela 轻帆 · 新邮件实时监控中",
			Menu: mygo.NewMenu([]*mygo.MenuItem{
				{Label: "显示主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { win.Show(); win.Focus() }},
				{Label: "立即同步", Click: func(*mygo.MenuItem, *mygo.Window) { win.Update(a.Sync) }},
				mygo.Separator(),
				{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { quitting = true; mygo.App.Quit() }},
			}),
		})
		if err != nil {
			// 没有托盘（如 Linux 缺 libayatana-appindicator3）时，关窗就真正退出
			log.Printf("[vela] 系统托盘不可用：%v", err)
			quitting = true
		}
		_ = tray
	})
	mygo.App.OnWindowAllClosed(func() { mygo.App.Quit() })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
