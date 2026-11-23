package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// 应用内更新：检查 GitHub Release 上的签名更新（mygo.Updater），下载、替换安装并重启。
// 真正的下载与签名校验在 main 注入的 Updater 里；这里只负责界面状态与流程。

// Updater 是应用内更新能力的抽象（生产环境由 main 用 mygo.Updater 实现）。
type Updater interface {
	// Enabled 表示当前安装方式能自行更新（开发版、包管理器安装的 .deb 不能）。
	Enabled() bool
	// Check 返回比当前版本新的版本；已是最新时返回 nil, nil。
	Check(ctx context.Context) (*UpdateInfo, error)
	// Install 下载并安装更新；progress 收到已下载与总字节数。
	Install(ctx context.Context, progress func(done, total int64)) error
	// Relaunch 重启应用，让新版本生效。
	Relaunch()
}

// UpdateInfo 是一个可安装的新版本。
type UpdateInfo struct {
	Version string
	Notes   string
}

type updateState struct {
	checking   bool
	latest     bool // 已是最新
	info       *UpdateInfo
	err        string
	installing bool
	progress   float64 // 0..1；负数表示总量未知
	done       bool
}

// SetUpdater 注入应用内更新能力。
func (a *App) SetUpdater(u Updater) { a.upd = u }

// SetVersion 注入当前版本号（main 取 mygo.App.Version()）。
func (a *App) SetVersion(v string) { a.version = v }

// CurrentVersion 返回展示用的版本号。
func (a *App) CurrentVersion() string {
	v := strings.TrimSpace(a.version)
	if v == "" {
		return "开发版"
	}
	return "v" + strings.TrimPrefix(v, "v")
}

// CheckUpdate 在后台检查更新并把结果写回界面状态。
func (a *App) CheckUpdate() { a.checkUpdate(false) }

// AutoCheckUpdate 在启动后静默检查一次，有新版本时只提示一句。
func (a *App) AutoCheckUpdate() {
	if a.upd == nil || !a.upd.Enabled() || a.updState.info != nil || a.updState.checking {
		return
	}
	a.checkUpdate(true)
}

func (a *App) checkUpdate(silent bool) {
	u := a.upd
	if u == nil || !u.Enabled() || a.updState.checking || a.updState.installing {
		return
	}
	a.updState = updateState{checking: true, progress: -1}
	a.run(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := u.Check(ctx)
		return func() {
			st := &a.updState
			st.checking = false
			if err != nil {
				if !silent {
					st.err = err.Error()
				}
				return
			}
			if info == nil {
				if !silent {
					st.latest = true
				}
				return
			}
			st.info = info
			if silent {
				a.toast("发现新版本 " + info.Version + "，可在「设置 → 关于与更新」中安装")
			}
		}
	})
}

// InstallUpdate 下载并安装已发现的新版本，成功后重启应用。
func (a *App) InstallUpdate() {
	u := a.upd
	st := &a.updState
	if u == nil || st.info == nil || st.installing || st.checking {
		return
	}
	st.installing, st.err, st.progress = true, "", -1
	a.run(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		var last time.Time
		err := u.Install(ctx, func(done, total int64) {
			p := -1.0
			if total > 0 {
				p = float64(done) / float64(total)
			}
			if time.Since(last) < 100*time.Millisecond && p < 1 {
				return // 进度不必每读一次都重绘
			}
			last = time.Now()
			a.post(func() { a.updState.progress = p })
		})
		return func() {
			st.installing = false
			if err != nil {
				st.err = err.Error()
				return
			}
			st.progress, st.done = 1, true
			u.Relaunch()
		}
	})
}

// updateView 渲染设置页的「关于与更新」分组。
func (a *App) updateView(c *ui.Context, p palette) {
	t := c.Theme()
	st := &a.updState
	enabled := a.upd != nil && a.upd.Enabled()
	ui.Column(c).Padding(14, 16).Gap(10).Radius(12).Background(p.FieldGroupBg).Border(1, p.SubtleDivider).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("sparkles")).FontSize(15).TextColor(t.Accent)
			ui.Text(c, "关于与更新").FontSize(14).FontWeight(600)
			ui.Text(c, "· Vela 轻帆 "+a.CurrentVersion()).FontSize(12).TextColor(t.TextMuted)
		})

		switch {
		case st.done:
			ui.Text(c, "更新已安装，正在重新启动…").FontSize(12).TextColor(t.TextMuted)
		case st.installing:
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Progress(c, st.progress).Grow(1)
				ui.Text(c, "正在下载并安装，完成后会自动重启").FontSize(12).TextColor(t.TextMuted)
			})
		case st.checking:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在检查更新…").FontSize(12).TextColor(t.TextMuted)
			})
		case st.info != nil:
			ui.Text(c, "发现新版本 "+st.info.Version).FontSize(13).FontWeight(500)
			if notes := strings.TrimSpace(st.info.Notes); notes != "" {
				ui.Scroll(c).MaxHeight(130).Children(func() {
					ui.Text(c, notes).FontSize(11.5).LineHeight(1.5).TextColor(t.TextMuted).Selectable()
				})
			}
		case st.latest:
			ui.Text(c, "已是最新版本。").FontSize(12).TextColor(t.TextMuted)
		case st.err != "":
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10, 12).Radius(10).
				Background(t.Danger.Alpha(0.10)).Border(1, t.Danger.Alpha(0.28)).Children(func() {
				ui.Text(c, st.err).FontSize(12.5).TextColor(t.Danger).Grow(1).Selectable()
			})
		default:
			if enabled {
				ui.Text(c, "检查 GitHub 上是否有新版本，下载安装后自动重启。").FontSize(12).LineHeight(1.5).TextColor(t.TextMuted)
			} else {
				ui.Text(c, "当前安装方式不支持应用内更新（开发版，或由系统包管理器安装的版本）。").FontSize(12).LineHeight(1.5).TextColor(t.TextMuted)
			}
		}

		if enabled && !st.done {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if st.info != nil && !st.installing && !st.checking {
					pressable(c, ui.PrimaryButton(c, "下载并安装 "+st.info.Version)).OnClick(a.InstallUpdate)
				}
				busy := st.checking || st.installing
				label := "检查更新"
				if st.info != nil {
					label = "重新检查"
				}
				pressable(c, ui.Button(c, label).Disabled(busy)).OnClick(a.CheckUpdate)
			})
		}
	})
}
