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
	"time"

	"github.com/azzliang6/vela/internal/app"
	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

//go:embed resources/tray.png
var trayIcon []byte

// webBody 是显示 HTML 正文的 WebView。
//
// Windows（WebView2）上正文空白的根因：之前用 LoadHTML(doc, "about:blank")。
// MyGo 的 Windows 后端在 baseURL 非空时不调 NavigateToString，而是先 Navigate 到
// baseURL，再靠 WebResourceRequested 过滤器把文档“回答”给那次请求；about:blank
// 不经过 WebResourceRequested，于是加载出来的永远是空白页。现在 baseURL 传空，
// 直接走 NavigateToString。另外加载失败、超时或文档过大时，回退为原生纯文本。
type webBody struct {
	wv      *mygo.WebView
	page    htmlLoader // wv.Page()；测试时替换
	setBg   func(string) error
	last    string // 当前文档的 key（邮件 id + 主题）
	gen     int
	loaded  bool
	post    func(func())
	onFail  func(id string)
	timeout time.Duration
}

func (b *webBody) Show(key, doc, bg string) bool {
	if b.page == nil || len(doc) > app.MaxDocBytes {
		return false
	}
	if b.last == key {
		return true
	}
	b.last, b.loaded = key, false
	b.gen++
	gen := b.gen
	id, _, _ := strings.Cut(key, "|")
	_ = b.setBg(bg)          // 与阅读区同色，避免白屏/黑屏闪烁
	b.page.LoadHTML(doc, "") // baseURL 必须为空，见上面的说明
	time.AfterFunc(b.timeout, func() {
		b.post(func() {
			if b.gen == gen && !b.loaded {
				b.fail(id)
			}
		})
	})
	return true
}

// htmlLoader 是 *mygo.Page 中正文用到的部分。
type htmlLoader interface{ LoadHTML(html, baseURL string) }

func (b *webBody) fail(id string) {
	log.Printf("[vela] 邮件 %s 的 HTML 正文未能在 WebView 中显示，改用纯文本", id)
	b.last = ""
	b.onFail(id)
}

func (b *webBody) Native() ui.NativeWebView { return b.wv }

func main() {
	mygo.App.SetName("Vela")
	be, err := core.OpenBackend()
	if err != nil {
		log.Fatal(err)
	}
	a := app.New(be, filepath.Join(core.DataDir(), "settings.json"))

	// 备份文件选择/保存走系统文件对话框
	backupFilter := []mygo.FileFilter{{Name: "Vela 备份", Extensions: []string{"velabak"}}}
	a.SetDialogs(app.FileDialogs{
		SaveFile: func(title, defaultName string) (string, bool) {
			p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: title, DefaultPath: defaultName, Filters: backupFilter})
			return p, err == nil && p != ""
		},
		OpenFile: func(title string) (string, bool) {
			ps, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, Filters: backupFilter})
			if err != nil || len(ps) == 0 {
				return "", false
			}
			return ps[0], true
		},
	})
	mygo.App.WhenReady(func() {
		// 主题来源同步到系统窗口外观：浅色主题下窗口边框/标题栏不再保持深色
		src := mygo.ThemeSource(a.Settings().Theme)
		if src == "" {
			src = mygo.ThemeSystem
		}
		mygo.Theme.SetSource(src)
		a.SetThemeChangeHook(func(theme string) {
			s := mygo.ThemeSource(theme)
			if s == "" {
				s = mygo.ThemeSystem
			}
			mygo.Theme.SetSource(s)
		})

		var win *mygo.Window
		var quitting bool
		win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Vela 轻帆", Width: 1280, Height: 820, MinWidth: 1080, MinHeight: 700,
			StateKey: "main", BackgroundColor: "light-dark(#f7f4ea, #141210)",
			// 无边框：应用自绘抬头并负责拖动，窗口控制按钮仍由系统提供
			TitleBarStyle: mygo.TitleBarHidden,
			Content:       ui.View(a.View),
		})
		// 关窗 = 隐藏到托盘，实时监控继续
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				win.Hide()
			}
		})

		body := &webBody{post: win.Update, timeout: 8 * time.Second}
		body.onFail = func(id string) { a.MarkBodyFailed(id); win.Invalidate() }
		if wv, err := win.NewWebView(mygo.WebViewOptions{BackgroundColor: app.WebViewBackground()}); err == nil {
			body.wv, body.page, body.setBg = wv, wv.Page(), wv.SetBackgroundColor
			wv.Page().OnDOMReady(func() { body.loaded = true })
			wv.Page().OnDidFinishLoad(func() { body.loaded = true })
			// 不监听 OnDidFailLoad：切换邮件时被取消的旧导航也会报失败；用超时判断即可。
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
