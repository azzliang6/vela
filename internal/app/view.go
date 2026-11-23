package app

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo/ui"
)

// View 构建整个窗口：亚麻侧边栏 | 棉纸邮件列表 | 象牙阅读信笺，外加五个弹窗面板。
func (a *App) View(c *ui.Context) {
	dark := c.Theme().Dark
	switch a.settings.Theme {
	case "light":
		dark = false
	case "dark":
		dark = true
	}
	a.dark = dark
	c.SetTheme(velaTheme(dark))
	t := c.Theme()
	p := velaPalette(dark)

	for _, m := range a.toasts {
		c.Toast(m)
	}
	a.toasts = a.toasts[:0]

	a.shortcuts(c)

	// 无边框窗口：顶部三栏抬头都可拖动窗口，并避让系统控制按钮
	bar := c.TitleBar()

	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		a.sidebar(c, p, bar)
		ui.Box(c).Width(1).Background(p.PaneDivider)
		a.mailList(c, p, bar)
		ui.Box(c).Width(1).Background(p.PaneDivider)
		a.viewer(c, p, bar)
	})

	velaModal(c, p, &a.composeOpen, 680, func() { a.composeView(c, p) })
	velaModal(c, p, &a.addOpen, 480, func() { a.addView(c, p) })
	velaModal(c, p, &a.settingsOpen, 540, func() { a.settingsView(c, p) })
	velaModal(c, p, &a.searchOpen, 580, func() { a.paletteView(c, p) })
	velaModal(c, p, &a.backupOpen, 500, func() { a.backupView(c, p) })
	if !a.composeOpen && a.compose != nil && !a.compose.Sending {
		a.composeClosed()
	}
	a.reportUnread()
}

func (a *App) shortcuts(c *ui.Context) {
	if a.composeOpen || a.addOpen || a.settingsOpen || a.searchOpen || a.backupOpen {
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		a.searchOpen, a.paletteQ = true, ""
	}
	if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.StartCompose("new")
	}
	if c.Shortcut(ui.Cmd, ui.KeyR) {
		a.Sync()
	}
}

/* ———— 通用视觉部件 ———— */

// velaModal 构建温润浮起的纸张弹窗：柔和暖遮罩 + 象牙卡片 + 细边与层次阴影。
func velaModal(c *ui.Context, p palette, open *bool, width float32, fn func()) {
	if !*open {
		return
	}
	t := c.Theme()
	ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(p.DialogBackdrop)
		shadowAlpha := float32(0.22)
		if t.Dark {
			shadowAlpha = 0.55
		}
		panel.Width(width).MaxWidthPercent(92).MaxHeightPercent(90).
			Radius(16).Background(p.CardBg).Border(1, p.DialogBorder).
			Shadow(0, 22, 52, -10, ui.RGBA(18, 14, 10, shadowAlpha)).Clip()
		dialogEnter(c, panel)
		fn()
	})
}

// ghostIconBtn 是无边框的清透图标按钮，平时静默，悬停或按下时浮现淡墨水洗底色。
func ghostIconBtn(c *ui.Context, p palette, name, label string, disabled bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(30, 30).Radius(8).Center().Label(label).Tooltip(label).Disabled(disabled)
	icColor := t.TextMuted
	if disabled {
		b.Opacity(0.4)
	} else if b.Pressed() {
		b.Background(p.PressWash)
		icColor = t.Text
	} else if b.Hovered() {
		b.Background(p.HoverWash)
		icColor = t.Text
	}
	b.Children(func() {
		ui.Icon(c, icon(name)).FontSize(16).TextColor(icColor)
	})
	colorFade(c, b)
	if disabled {
		return b
	}
	return pressable(c, b)
}

// senderAvatar 绘制矿物纸墨配色的圆形发件人头像。
func senderAvatar(c *ui.Context, name, email string, size float32, dark bool) ui.Element {
	seed := nonEmpty(name, email)
	bg, fg := avatarTone(seed, dark)
	av := ui.Box(c).Size(size, size).Radius(size / 2).Shrink(0).Center().
		Background(bg).Role(ui.RoleImage).Label(seed)
	av.Children(func() {
		ui.Text(c, senderInitial(name, email)).FontSize(size * 0.42).FontWeight(600).TextColor(fg).SingleLine()
	})
	return av
}

// searchBar 构建支持中文占位符、聚焦暖光环、清空按钮与可选命令面板快捷键键帽的搜索框。
func searchBar(c *ui.Context, p palette, query *string, placeholder, label string, autoFocus bool, onPalette func()) ui.Element {
	t := c.Theme()
	row := ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 12).Radius(10).Background(p.CardBg)
	var in ui.Element
	row.Children(func() {
		ui.Icon(c, icon("search")).FontSize(15).Shrink(0).TextColor(t.TextMuted)
		in = ui.TextInputBase(c, query).Grow(1).MinWidth(60).Placeholder(placeholder).Label(label).FocusRing(false)
		if autoFocus {
			in.AutoFocus()
		}
		if *query != "" {
			in.OnShortcut(0, ui.KeyEscape, func() { *query = "" })
			clr := ui.ButtonBase(c).Size(18, 18).Radius(9).Shrink(0).Center().
				Background(p.HoverWash).Label("清空搜索").Tooltip("清空")
			clr.Children(func() {
				ui.Icon(c, icon("x")).FontSize(11).TextColor(t.TextMuted)
			})
			clr.OnClick(func() { *query = "" })
		} else if onPalette != nil {
			kb := ui.ButtonBase(c).Padding(2, 6).Radius(5).Shrink(0).
				Background(p.KeycapBg).Label("命令面板").Tooltip("打开命令面板 (" + shortcut("K") + ")")
			kb.Children(func() {
				ui.Text(c, shortcut("K")).FontSize(11).TextColor(t.TextMuted).SingleLine()
			})
			kb.OnClick(onPalette)
		}
	})
	switch {
	case row.FocusWithin():
		row.Border(1, t.Accent).Shadow(0, 0, 0, 2, t.Accent.Alpha(0.14))
	case row.Hovered():
		row.Border(1, t.Border.Mix(t.TextMuted, 0.25))
	default:
		row.Border(1, t.Border)
	}
	colorFade(c, row)
	return in
}

func keycapHint(c *ui.Context, p palette, key, desc string) {
	t := c.Theme()
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Padding(2, 7).Radius(5).Background(p.KeycapBg).Children(func() {
			ui.Text(c, key).FontSize(11).FontWeight(600).TextColor(p.TextSoft).SingleLine()
		})
		ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted).SingleLine()
	})
}

// shortcut 返回快捷键的展示写法，与 ui.Cmd 的实际按键一致：macOS 是 ⌘N，其它平台是 Ctrl+N。
func shortcut(key string) string {
	if runtime.GOOS == "darwin" {
		return "⌘" + key
	}
	return "Ctrl+" + key
}

/* ———— 左侧边栏 ———— */

func (a *App) sidebar(c *ui.Context, p palette, bar ui.TitleBar) {
	t := c.Theme()
	brandPadL := float32(6)
	if bar.Height > 0 {
		brandPadL = max(6, bar.Left-12) // 避让 macOS 红绿灯
	}
	ui.Column(c).Width(248).Shrink(0).Padding(18, 12, 14).Gap(6).Background(p.RailBg).Children(func() {
		// 品牌抬头（空隙处可拖动窗口）
		ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(2, 6, 10, brandPadL).DragWindow().Children(func() {
			ui.Box(c).Size(30, 30).Radius(8).Shrink(0).Center().Background(t.Accent).
				Shadow(0, 2, 6, -1, t.Accent.Alpha(0.35)).Children(func() {
				ui.Icon(c, icon("sailboat")).FontSize(16).TextColor(t.AccentText)
			})
			ui.Text(c, "Vela 轻帆").FontSize(17).Bold().LetterSpacing(-0.2).Grow(1)
			ghostIconBtn(c, p, "settings", "设置", false).OnClick(func() { a.settingsOpen = true })
		})

		// 写邮件主按钮
		composeBtn := ui.PrimaryButton(c, "").Label("写邮件").Padding(10, 14).Radius(10).
			Shadow(0, 4, 12, -2, t.Accent.Alpha(0.30)).Children(func() {
			ui.Icon(c, icon("square-pen")).FontSize(15)
			ui.Text(c, "写邮件").FontWeight(600).SingleLine()
			ui.Spacer(c)
			ui.Text(c, shortcut("N")).FontSize(11).TextColor(t.AccentText.Alpha(0.75)).SingleLine()
		}).OnClick(func() { a.StartCompose("new") })
		pressable(c, composeBtn).Margin(0, 2, 8)

		// 文件夹与智能分类滚动区
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				for _, f := range Folders {
					a.navItem(c, p, f.ID, f.Label, folderIcon(f.ID), a.Unread(f.ID))
				}
				ui.Text(c, "智能分类").FontSize(11.5).FontWeight(600).LetterSpacing(0.4).
					TextColor(t.TextMuted).Padding(18, 10, 6)
				for _, f := range Categories {
					a.categoryItem(c, p, f.ID, f.Label)
				}
			})
		})

		// 底部账户与同步状态区
		ui.Box(c).Height(1).Background(p.SubtleDivider).Margin(4, 4, 2)
		ui.Row(c).AlignItems(ui.Center).Padding(4, 8, 2).Children(func() {
			ui.Text(c, "邮箱账户").FontSize(11.5).FontWeight(600).LetterSpacing(0.4).TextColor(t.TextMuted).Grow(1)
			ghostIconBtn(c, p, "plus", "添加邮箱", false).OnClick(func() { a.addOpen = true })
		})
		ui.Column(c).Gap(2).Children(func() {
			a.accountItem(c, p, "", "全部账户")
			for _, acc := range a.accounts {
				a.accountItem(c, p, acc.ID, acc.Label)
			}
		})

		// 同步底栏胶囊
		ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(6, 2, 0).Padding(5, 8, 5, 10).
			Radius(9).Background(p.HoverWash).Children(func() {
			dotColor := t.TextMuted.Alpha(0.55)
			label := "未同步"
			if a.syncing {
				dotColor = t.Warning
				label = "同步中…"
			} else if !a.syncedAt.IsZero() {
				dotColor = t.Success
				label = "同步于 " + a.syncedAt.Format("15:04")
			}
			ui.Box(c).Size(6, 6).Radius(3).Shrink(0).Background(dotColor)
			ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).Grow(1).SingleLine()
			ghostIconBtn(c, p, "refresh-cw", "立即同步", a.syncing || len(a.accounts) == 0).OnClick(a.Sync)
		})
	})
}

func folderIcon(id string) string {
	return map[string]string{"inbox": "inbox", "starred": "star", "sent": "send", "drafts": "file-text", "archive": "archive", "trash": "trash-2"}[id]
}

func categoryLabel(id string) string {
	for _, f := range Categories {
		if f.ID == id {
			return f.Label
		}
	}
	return ""
}

func (a *App) navItem(c *ui.Context, p palette, id, label, ic string, count int) {
	t := c.Theme()
	on := a.folder == id
	row := ui.Row(c.Key("nav-" + id)).Gap(10).AlignItems(ui.Center).Padding(8, 11).Radius(9).Role(ui.RoleButton).Label(label)
	icCol := t.TextMuted
	txtCol := p.TextSoft
	if on {
		icCol = t.Accent
		txtCol = t.Text
	}
	row.Children(func() {
		ui.Icon(c, icon(ic)).FontSize(16).Shrink(0).TextColor(icCol)
		lbl := ui.Text(c, label).FontSize(13.5).TextColor(txtCol).Grow(1).SingleLine()
		if on {
			lbl.FontWeight(600)
		}
		if count > 0 {
			badgeBg, badgeFg := p.HoverWash, p.TextSoft
			if on || id == "inbox" {
				badgeBg, badgeFg = p.AccentSoft, t.Accent
			}
			ui.Box(c).Padding(1, 7).Radius(999).Shrink(0).Background(badgeBg).Children(func() {
				ui.Text(c, fmt.Sprint(count)).FontSize(11.5).FontWeight(600).TextColor(badgeFg).SingleLine()
			})
		}
	})
	styleNavPill(c, p, row, on)
	row.OnClick(func() { a.SetFolder(id) })
}

func (a *App) categoryItem(c *ui.Context, p palette, id, label string) {
	t := c.Theme()
	on := a.folder == id
	dot := categoryColor(id, t.Dark)
	row := ui.Row(c.Key("nav-" + id)).Gap(11).AlignItems(ui.Center).Padding(7, 13).Radius(9).Role(ui.RoleButton).Label(label)
	txtCol := p.TextSoft
	if on {
		txtCol = t.Text
	}
	row.Children(func() {
		d := ui.Box(c).Size(8, 8).Radius(4).Shrink(0).Background(dot)
		if on {
			d.Shadow(0, 0, 0, 2, dot.Alpha(0.22))
		}
		lbl := ui.Text(c, label).FontSize(13.5).TextColor(txtCol).Grow(1).SingleLine()
		if on {
			lbl.FontWeight(600)
		}
	})
	styleNavPill(c, p, row, on)
	row.OnClick(func() { a.SetFolder(id) })
}

func (a *App) accountItem(c *ui.Context, p palette, id, label string) {
	t := c.Theme()
	on := a.account == id
	row := ui.Row(c.Key("acc-" + id)).Gap(9).AlignItems(ui.Center).Padding(7, 11).Radius(9).Role(ui.RoleButton).Label(label)
	txtCol := p.TextSoft
	if on {
		txtCol = t.Text
	}
	row.Children(func() {
		if id == "" {
			icCol := t.TextMuted
			if on {
				icCol = t.Accent
			}
			ui.Icon(c, icon("mail")).FontSize(15).Shrink(0).TextColor(icCol)
		} else {
			avBg, avFg := avatarTone(id, t.Dark)
			ui.Box(c).Size(20, 20).Radius(6).Shrink(0).Center().Background(avBg).Children(func() {
				ui.Text(c, senderInitial(id, label)).FontSize(11).FontWeight(700).TextColor(avFg).SingleLine()
			})
		}
		lbl := ui.Text(c, label).FontSize(13).TextColor(txtCol).Grow(1).MaxLines(1).Ellipsis("…")
		if on {
			lbl.FontWeight(600)
		}
	})
	styleNavPill(c, p, row, on)
	row.OnClick(func() { a.account, a.selected = id, "" })
	if id != "" {
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("移除此账户").Chosen() {
				a.RemoveAccount(id)
			}
		})
	}
}

// styleNavPill 统一处理侧栏条目的浮起选中胶囊与淡墨水洗悬停态。
func styleNavPill(c *ui.Context, p palette, e ui.Element, on bool) {
	t := c.Theme()
	switch {
	case on:
		e.Background(p.NavActiveBg).Border(1, p.NavActiveBorder)
		if !t.Dark {
			e.Shadow(0, 1, 3, 0, ui.RGBA(29, 26, 21, 0.05))
		}
	case e.Pressed():
		e.Background(p.PressWash)
	case e.Hovered():
		e.Background(p.HoverWash)
	}
	colorFade(c, e)
}

func hoverBG(c *ui.Context, p palette, e ui.Element, on bool) {
	switch {
	case on:
		e.Background(p.CardSelected)
	case e.Pressed():
		e.Background(p.PressWash)
	case e.Hovered():
		e.Background(p.HoverWash)
	}
	colorFade(c, e)
}

/* ———— 中栏：邮件列表（虚拟列表：只构建可见行）———— */

func (a *App) folderLabel() string {
	for _, f := range append(Folders, Categories...) {
		if f.ID == a.folder {
			return f.Label
		}
	}
	return a.folder
}

func (a *App) mailList(c *ui.Context, p palette, bar ui.TitleBar) {
	t := c.Theme()
	items := a.Visible()
	ui.Column(c).Width(396).Shrink(0).AlignItems(ui.Stretch).Background(p.ListBg).Children(func() {
		// 列表栏标题与计数胶囊（空隙处可拖动窗口）
		ui.Row(c).Padding(20, 18, 12).Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, a.folderLabel()).FontSize(20).Bold().LetterSpacing(-0.2).Grow(1)
			ui.Box(c).Padding(2, 9).Radius(999).Background(p.HoverWash).Children(func() {
				ui.Text(c, fmt.Sprintf("%d 封", len(items))).FontSize(11.5).FontWeight(500).TextColor(p.TextSoft).SingleLine()
			})
		})

		// 中文搜索框
		ui.Column(c).Padding(0, 14, 12).AlignItems(ui.Stretch).Children(func() {
			searchBar(c, p, &a.query, "搜索主题、发件人、正文…", "搜索", false, func() {
				a.searchOpen, a.paletteQ = true, ""
			})
		})
		ui.Box(c).Height(1).Background(p.SubtleDivider)

		listBox := ui.Column(c.Key("list-" + a.folder + a.account)).Grow(1).AlignItems(ui.Stretch).Padding(6, 0)
		folderSwitch(c, listBox)
		listBox.Children(func() {
			state := &ui.ListState{
				Key:   func(i int) any { return items[i].ID },
				Label: func(i int) string { return items[i].Subject },
			}
			ui.List(c, state, len(items), func(i int) {
				hideSep := i == len(items)-1 || a.selected == items[i].ID || (i+1 < len(items) && a.selected == items[i+1].ID)
				a.mailRow(c, p, items[i], hideSep)
			}).Grow(1).Children(func() {
				if len(items) == 0 {
					msg := "这里空空如也"
					sub := "当前列表没有匹配的信件"
					ic := "inbox"
					if len(a.accounts) == 0 {
						msg = "还没有邮箱账户，点左下角 + 添加"
						sub = "支持 QQ、163、Gmail、Outlook、iCloud 与自定义 IMAP"
						ic = "mail"
					} else if strings.TrimSpace(a.query) != "" {
						sub = "换个关键词试试，或按 Esc 清空搜索"
						ic = "search"
					}
					ui.Column(c).FillWidth().Padding(64, 24).Center().Gap(10).Children(func() {
						ui.Box(c).Size(48, 48).Radius(24).Center().Background(p.CardBg).Border(1, p.SubtleDivider).Children(func() {
							ui.Icon(c, icon(ic)).FontSize(20).TextColor(t.TextMuted)
						})
						ui.Text(c, msg).FontSize(14).FontWeight(600).TextColor(p.TextSoft).TextAlign(ui.Center)
						ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
					})
				}
			})
		})
	})
}

func (a *App) mailRow(c *ui.Context, p palette, e *core.Email, hideSep bool) {
	t := c.Theme()
	compact := a.settings.Density == "compact"
	padY := float32(12)
	avSize := float32(34)
	if compact {
		padY = 8
		avSize = 26
	}
	on := a.selected == e.ID
	id := e.ID

	ui.Column(c).Padding(2, 10).AlignItems(ui.Stretch).Children(func() {
		card := ui.Row(c).Gap(11).AlignItems(ui.Start).Padding(padY, 13).Radius(11).Role(ui.RoleButton).Label(e.Subject)
		card.Children(func() {
			senderAvatar(c, e.From.Name, e.From.Email, avSize, t.Dark).Margin(2, 0, 0, 0)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				// 第一行：未读圆点 + 发件人 + 星标/附件 + 日期
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if !e.Read {
						ui.Box(c).Size(7, 7).Radius(4).Shrink(0).Background(t.Accent)
					}
					from := ui.Text(c, nonEmpty(e.From.Name, e.From.Email)).FontSize(13.5).Grow(1).MaxLines(1).Ellipsis("…")
					if !e.Read {
						from.Bold().TextColor(t.Text)
					} else {
						from.FontWeight(500).TextColor(p.TextSoft)
					}
					if e.Starred {
						ui.Icon(c, icon("star")).FontSize(12).Shrink(0).TextColor(starColor[t.Dark])
					}
					if len(e.Attachments) > 0 {
						ui.Icon(c, icon("paperclip")).FontSize(12).Shrink(0).TextColor(t.TextMuted)
					}
					ui.Text(c, shortDate(e.Date)).FontSize(11.5).Shrink(0).TextColor(t.TextMuted)
				})

				// 第二行：主题
				subj := ui.Text(c, nonEmpty(e.Subject, "（无主题）")).FontSize(13.5).MaxLines(1).Ellipsis("…")
				if !e.Read {
					subj.FontWeight(600).TextColor(t.Text)
				} else {
					subj.TextColor(p.TextSoft)
				}

				// 第三行（舒适模式）：摘要与智能标签
				if !compact {
					preview := e.AISummary
					hasAI := preview != ""
					if !hasAI {
						preview = previewText(e.Body)
					}
					if preview != "" {
						ui.Text(c, preview).FontSize(12.5).LineHeight(1.42).TextColor(t.TextMuted).MaxLines(2).Ellipsis("…")
					}
					cat := categoryLabel(e.Category)
					if hasAI || (cat != "" && e.Category != "spam") {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(2, 0, 0, 0).Children(func() {
							if hasAI {
								ui.Box(c).Padding(1, 7).Radius(999).Background(p.AccentSoft).Children(func() {
									ui.Text(c, "✦ AI 摘要").FontSize(11).FontWeight(600).TextColor(t.Accent).SingleLine()
								})
							}
							if cat != "" && e.Category != "spam" {
								dot := categoryColor(e.Category, t.Dark)
								ui.Row(c).Gap(4).AlignItems(ui.Center).Padding(1, 7).Radius(999).Background(p.HoverWash).Children(func() {
									ui.Box(c).Size(5, 5).Radius(3).Background(dot)
									ui.Text(c, cat).FontSize(11).TextColor(p.TextSoft).SingleLine()
								})
							}
						})
					}
				}
			})
		})

		switch {
		case on:
			card.Background(p.CardSelected).Border(1, p.CardSelectedBorder)
			if !t.Dark {
				card.Shadow(0, 2, 8, -2, ui.RGBA(29, 26, 21, 0.06))
			}
			accent := t.Accent
			card.DrawOver(func(pt *ui.Painter, r ui.Rect) {
				barH := min(max(r.H*0.34, 18), 28)
				pt.Fill(ui.Rect{X: r.X + 4, Y: r.Y + (r.H-barH)/2, W: 3, H: barH}, accent, 1.5)
			})
		case card.Pressed():
			card.Background(p.PressWash)
		case card.Hovered():
			card.Background(p.HoverWash)
		default:
			if !hideSep {
				sep := p.SubtleDivider
				inset := avSize + 24
				card.DrawOver(func(pt *ui.Painter, r ui.Rect) {
					if r.W > inset+12 {
						pt.Line(r.X+inset, r.Y+r.H+2, r.X+r.W-12, r.Y+r.H+2, 1, sep)
					}
				})
			}
		}
		colorFade(c, card)
		a.arrivalHighlight(c, card, e.ID, on)
		card.OnClick(func() { a.Select(id) })
		card.ContextMenu(func(m *ui.Menu) {
			if m.Item(map[bool]string{true: "标为未读", false: "标为已读"}[e.Read]).Chosen() {
				a.ToggleRead(id)
			}
			if m.Item(map[bool]string{true: "取消星标", false: "加星标"}[e.Starred]).Chosen() {
				a.ToggleStar(id)
			}
			m.Separator()
			if m.Item("归档").Chosen() {
				a.Move(id, "archive")
			}
			if m.Item("删除").Chosen() {
				a.Move(id, "trash")
			}
		})
	})
}

func nonEmpty(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func shortDate(s string) string {
	d, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	d = d.Local()
	now := time.Now()
	switch {
	case d.YearDay() == now.YearDay() && d.Year() == now.Year():
		return d.Format("15:04")
	case d.Year() == now.Year():
		return d.Format("1月2日")
	}
	return d.Format("2006/1/2")
}

/* ———— 右栏：阅读区 ———— */

func (a *App) viewer(c *ui.Context, p palette, bar ui.TitleBar) {
	t := c.Theme()
	e := a.Selected()
	ui.Column(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		if e == nil {
			ui.Column(c.Key("empty")).Grow(1).Center().Gap(14).Padding(32).Children(func() {
				// 无边框时顶部留白可拖动窗口
				dragPad := float32(0)
				if bar.Height > 0 {
					dragPad = max(0, bar.Height-32)
				}
				if dragPad > 0 {
					ui.Row(c).Height(dragPad).FillWidth().DragWindow()
				}
				ui.Box(c).Size(68, 68).Radius(34).Center().
					Background(p.CardBg).Border(1, p.SubtleDivider).
					Shadow(0, 4, 14, -4, ui.RGBA(29, 26, 21, 0.06)).Children(func() {
					ui.Icon(c, icon("sailboat")).FontSize(28).TextColor(t.Accent.Alpha(0.8))
				})
				ui.Column(c).Center().Gap(4).Children(func() {
					ui.Text(c, "选择一封邮件开始阅读").FontSize(16).FontWeight(600).TextColor(p.TextSoft)
					ui.Text(c, "静下心读信、回信，把收件箱当作一张整洁的书桌").FontSize(12.5).TextColor(t.TextMuted)
				})
				ui.Row(c).Gap(14).AlignItems(ui.Center).Margin(6, 0, 0, 0).Children(func() {
					keycapHint(c, p, shortcut("K"), "搜索")
					keycapHint(c, p, shortcut("N"), "写邮件")
					keycapHint(c, p, shortcut("R"), "同步")
				})
			})
			return
		}
		pane := ui.Column(c.Key("mail-" + e.ID)).Grow(1).AlignItems(ui.Stretch)
		detailEnter(c, pane)
		pane.Children(func() { a.detail(c, p, e, bar) })
	})
}

func (a *App) detail(c *ui.Context, p palette, e *core.Email, bar ui.TitleBar) {
	t := c.Theme()
	id := e.ID

	// 顶部操作栏（与正文 36px 左右边距严格对齐；右侧避让窗口控制按钮，空隙可拖动窗口）
	toolR := float32(36)
	if bar.Height > 0 {
		toolR = max(36, bar.Right+12)
	}
	ui.Row(c).Padding(12, toolR, 12, 36).Gap(4).AlignItems(ui.Center).DragWindow().Children(func() {
		if e.Folder == "drafts" {
			// 草稿：主按钮是继续编辑，回复 / 转发对自己的草稿没有意义
			editBtn := ui.ButtonBase(c).Padding(6, 12).Gap(6).Radius(8).
				Background(p.CardBg).Border(1, t.Border).Label("继续编辑").Tooltip("在写信窗口里继续编辑这封草稿")
			if editBtn.Pressed() {
				editBtn.Background(t.SurfacePressed)
			} else if editBtn.Hovered() {
				editBtn.Background(t.SurfaceHover)
			}
			editBtn.Children(func() {
				ui.Icon(c, icon("square-pen")).FontSize(15).TextColor(p.TextSoft)
				ui.Text(c, "继续编辑").FontSize(13).FontWeight(500).TextColor(t.Text).SingleLine()
			})
			colorFade(c, editBtn)
			pressable(c, editBtn).Margin(0, 2, 0, 0).OnClick(func() { a.OpenDraft(id) })
		} else {
			replyBtn := ui.ButtonBase(c).Padding(6, 12).Gap(6).Radius(8).
				Background(p.CardBg).Border(1, t.Border).Label("回复").Tooltip("回复")
			if replyBtn.Pressed() {
				replyBtn.Background(t.SurfacePressed)
			} else if replyBtn.Hovered() {
				replyBtn.Background(t.SurfaceHover)
			}
			replyBtn.Children(func() {
				ui.Icon(c, icon("reply")).FontSize(15).TextColor(p.TextSoft)
				ui.Text(c, "回复").FontSize(13).FontWeight(500).TextColor(t.Text).SingleLine()
			})
			colorFade(c, replyBtn)
			pressable(c, replyBtn).Margin(0, 2, 0, 0).OnClick(func() { a.StartCompose("reply") })

			ghostIconBtn(c, p, "reply-all", "回复全部", false).OnClick(func() { a.StartCompose("replyAll") })
			ghostIconBtn(c, p, "forward", "转发", false).OnClick(func() { a.StartCompose("forward") })
		}
		ui.Box(c).Width(1).Height(16).Background(p.PaneDivider).Margin(0, 8)
		ghostIconBtn(c, p, "archive", "归档此邮件", false).OnClick(func() { a.Move(id, "archive") })
		ghostIconBtn(c, p, "trash-2", "删除此邮件", false).OnClick(func() { a.Move(id, "trash") })
		starIcon := map[bool]string{true: "star-off", false: "star"}[e.Starred]
		starLabel := map[bool]string{true: "取消星标", false: "加星标"}[e.Starred]
		ghostIconBtn(c, p, starIcon, starLabel, false).OnClick(func() { a.ToggleStar(id) })
		ghostIconBtn(c, p, "mail-open", map[bool]string{true: "标为未读", false: "标为已读"}[e.Read], false).OnClick(func() { a.ToggleRead(id) })

		ui.Spacer(c)

		label := "AI 分析"
		if a.aiLoading == id {
			label = "分析中…"
		}
		aiDisabled := a.aiLoading != ""
		aiBtn := ui.ButtonBase(c).Padding(6, 13).Gap(6).Radius(999).
			Background(p.AccentSoft).Border(1, p.AccentBorder).Label(label).Disabled(aiDisabled)
		if aiDisabled {
			aiBtn.Opacity(0.55)
		} else if aiBtn.Pressed() {
			aiBtn.Background(t.Accent.Alpha(0.22))
		} else if aiBtn.Hovered() {
			aiBtn.Background(t.Accent.Alpha(0.16))
		}
		aiBtn.Children(func() {
			ui.Icon(c, icon("sparkles")).FontSize(14).TextColor(t.Accent)
			ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(t.Accent).SingleLine()
		})
		colorFade(c, aiBtn)
		if !aiDisabled {
			pressable(c, aiBtn)
		}
		aiBtn.OnClick(func() { a.Analyze(id) })
	})
	ui.Box(c).Height(1).Background(p.SubtleDivider)

	// HTML 正文：抬头（主题、发件人、AI 摘要、附件）也渲染进同一个文档，
	// 与正文一起滚动，WebView 直接铺满阅读区剩余部分，没有任何边框或内边距容器。
	htmlSrc, ready := a.htmlFor(e)
	if !ready {
		ui.Box(c).Grow(1) // 正在从本地库读取 HTML 正文（通常只要几毫秒），读完自动刷新
		return
	}
	if htmlSrc != "" && a.body != nil && !a.bodyFailed[e.ID] {
		th := BodyThemeFor(t)
		doc, key := a.mailDocument(e.ID, htmlSrc, th, a.headerInfo(e))
		if a.body.Show(e.ID+"|"+key, doc, th.Bg) {
			ui.WebView(c, a.body.Native()).Grow(1)
			return
		}
	}
	text := e.Body
	if strings.TrimSpace(text) == "" && htmlSrc != "" {
		text = core.HTMLToText(htmlSrc)
	}
	// 纯文本：抬头与正文在同一个原生滚动视图里，保持舒适的书页行宽
	ui.Scroll(c).Grow(1).Children(func() {
		a.nativeHeader(c, p, e)
		ui.Column(c).MaxWidth(820).Padding(22, 36, 44).Gap(14).Children(func() {
			for _, para := range strings.Split(strings.TrimSpace(text), "\n\n") {
				if strings.TrimSpace(para) == "" {
					continue
				}
				ui.Text(c, para).FontSize(14.5).LineHeight(1.75).Selectable()
			}
		})
	})
}

// nativeHeader 是纯文本正文的原生信笺抬头。
func (a *App) nativeHeader(c *ui.Context, p palette, e *core.Email) {
	t := c.Theme()
	ui.Column(c).MaxWidth(820).Padding(28, 36, 20).Gap(14).Children(func() {
		ui.Text(c, nonEmpty(e.Subject, "（无主题）")).FontSize(23).Bold().LineHeight(1.35).LetterSpacing(-0.2).Selectable()

		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			senderAvatar(c, e.From.Name, e.From.Email, 38, t.Dark)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
					ui.Text(c, nonEmpty(e.From.Name, e.From.Email)).FontSize(14).FontWeight(600).Selectable()
					if e.From.Email != "" && e.From.Name != "" {
						ui.Text(c, "<"+e.From.Email+">").FontSize(13).TextColor(t.TextMuted).Selectable()
					}
				})
				var to []string
				for _, x := range e.To {
					to = append(to, x.Email)
				}
				line := "收件人：" + strings.Join(to, ", ")
				if len(e.CC) > 0 {
					var cc []string
					for _, x := range e.CC {
						cc = append(cc, x.Email)
					}
					line += "   抄送：" + strings.Join(cc, ", ")
				}
				ui.Text(c, line).FontSize(12).TextColor(t.TextMuted).MaxLines(2).Ellipsis("…")
			})
			if d, err := time.Parse(time.RFC3339, e.Date); err == nil {
				ui.Text(c, d.Local().Format("2006年1月2日 15:04")).FontSize(12).Shrink(0).TextColor(t.TextMuted)
			}
		})

		if e.AISummary != "" {
			ui.Column(c).Padding(12, 16).Gap(5).Radius(11).
				Background(p.AccentSoft).Border(1, p.AccentBorder).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icon("sparkles")).FontSize(13).TextColor(t.Accent)
					cat := ""
					for _, k := range Categories {
						if k.ID == e.Category {
							cat = " · " + k.Label
						}
					}
					ui.Text(c, "AI 摘要"+cat).FontSize(12).Bold().TextColor(t.Accent)
				})
				ui.Text(c, e.AISummary).FontSize(13.5).LineHeight(1.55).Selectable()
				if e.AIWhy != "" {
					ui.Text(c, e.AIWhy).FontSize(12).TextColor(t.TextMuted)
				}
			})
		}

		if len(e.Attachments) > 0 {
			ui.Row(c).Gap(8).Wrap().Children(func() {
				for _, at := range e.Attachments {
					ui.Row(c.Key(at.ID)).Gap(6).AlignItems(ui.Center).Padding(6, 12).
						Radius(9).Background(p.CardBg).Border(1, t.Border).Children(func() {
						ui.Icon(c, icon("paperclip")).FontSize(13).TextColor(t.TextMuted)
						ui.Text(c, at.Name).FontSize(12.5).FontWeight(500)
						ui.Text(c, formatSize(at.SizeKB)).FontSize(11.5).TextColor(t.TextMuted)
					}).Tooltip("附件内容未下载（与原版相同，只同步附件信息）")
				}
			})
		}

		ui.Box(c).Height(1).Background(p.SubtleDivider).Margin(4, 0, 0, 0)
	})
}

// headerInfo 是 HTML 文档里的抬头内容（与 nativeHeader 一致）。
func (a *App) headerInfo(e *core.Email) MailHeader {
	h := MailHeader{Subject: nonEmpty(e.Subject, "（无主题）"), FromName: e.From.Name, FromEmail: e.From.Email,
		Summary: e.AISummary, Why: e.AIWhy}
	var to []string
	for _, x := range e.To {
		to = append(to, x.Email)
	}
	h.Recipients = "收件人：" + strings.Join(to, ", ")
	if len(e.CC) > 0 {
		var cc []string
		for _, x := range e.CC {
			cc = append(cc, x.Email)
		}
		h.Recipients += "  抄送：" + strings.Join(cc, ", ")
	}
	if d, err := time.Parse(time.RFC3339, e.Date); err == nil {
		h.Date = d.Local().Format("2006年1月2日 15:04")
	}
	if e.AISummary != "" {
		h.SummaryTitle = "AI 摘要"
		for _, k := range Categories {
			if k.ID == e.Category {
				h.SummaryTitle += " · " + k.Label
			}
		}
	}
	for _, at := range e.Attachments {
		h.Attachments = append(h.Attachments, at.Name+"  "+formatSize(at.SizeKB))
	}
	return h
}

func formatSize(kb int) string {
	if kb >= 1024 {
		return fmt.Sprintf("%.1f MB", float64(kb)/1024)
	}
	return fmt.Sprintf("%d KB", max(kb, 1))
}

/* ———— 弹窗头部共用小部件 ———— */

func modalHeader(c *ui.Context, p palette, icName, title, subtitle string, onClose func()) {
	t := c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(20, 24, 14).Children(func() {
		ui.Box(c).Size(32, 32).Radius(9).Shrink(0).Center().Background(p.AccentSoft).Children(func() {
			ui.Icon(c, icon(icName)).FontSize(16).TextColor(t.Accent)
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(17).Bold()
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(12).TextColor(t.TextMuted)
			}
		})
		ghostIconBtn(c, p, "x", "关闭", false).OnClick(onClose)
	})
}

/* ———— 写信 ———— */

var composeTitle = map[string]string{"new": "写邮件", "reply": "回复", "replyAll": "回复全部", "forward": "转发"}

func (a *App) composeView(c *ui.Context, p palette) {
	t := c.Theme()
	cp := a.compose
	if cp == nil {
		return
	}
	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		modalHeader(c, p, "square-pen", composeTitle[cp.Mode], "", func() { a.composeOpen = false })

		// 信头一体化卡片：收件人 / 抄送 / 主题通过细分割线串联，告别沉重的多层输入框套框
		ui.Column(c).Margin(0, 24).Radius(12).Background(p.FieldGroupBg).Border(1, t.Border).Children(func() {
			if len(a.accounts) > 1 {
				var ids []string
				for _, acc := range a.accounts {
					ids = append(ids, acc.ID)
				}
				ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(8, 14).Children(func() {
					ui.Text(c, "发件人").Width(52).FontSize(13).TextColor(t.TextMuted)
					ui.Select(c, &cp.AccountID, ids).Grow(1).Label("发件人")
				})
				ui.Box(c).Height(1).Background(p.SubtleDivider)
			}
			lineField := func(label, placeholder string, v *string, focus, bold bool) {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(10, 14).Children(func() {
					ui.Text(c, label).Width(52).FontSize(13).TextColor(t.TextMuted)
					in := ui.TextInputBase(c, v).Grow(1).Placeholder(placeholder).Label(label).FocusRing(false)
					if bold {
						in.FontWeight(600)
					}
					if focus {
						in.AutoFocus()
					}
				})
			}
			lineField("收件人", "example@domain.com（多个地址用逗号分隔）", &cp.To, cp.Mode == "new" || cp.Mode == "forward", false)
			ui.Box(c).Height(1).Background(p.SubtleDivider)
			lineField("抄送", "可选", &cp.CC, false, false)
			ui.Box(c).Height(1).Background(p.SubtleDivider)
			lineField("主题", "邮件主题", &cp.Subject, false, true)
		})

		// 正文信笺输入区
		ui.Column(c).Margin(12, 24, 0).AlignItems(ui.Stretch).Children(func() {
			ta := ui.TextArea(c, &cp.Body).Height(280).LineHeight(1.65).Label("正文").Placeholder("写点什么…")
			if cp.Mode == "reply" || cp.Mode == "replyAll" {
				ta.AutoFocus()
			}
		})

		// 底栏：左侧存草稿，右侧舍弃与发送
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(16, 24, 20).Children(func() {
			pressable(c, ui.Button(c, "存草稿")).OnClick(a.SaveDraft)
			ui.Spacer(c)
			pressable(c, ui.Button(c, "舍弃").Tooltip("关闭且不保存这次的改动")).OnClick(a.DiscardCompose)
			label := "发送"
			if cp.Sending {
				label = "发送中…"
			}
			sendBtn := ui.PrimaryButton(c, "").Label(label).Padding(7, 16).Disabled(cp.Sending).Children(func() {
				ui.Icon(c, icon("send")).FontSize(14)
				ui.Text(c, label).FontWeight(600).SingleLine()
			})
			pressable(c, sendBtn).OnClick(a.SendCompose)
		})
	})
}

/* ———— 添加邮箱 ———— */

func (a *App) addView(c *ui.Context, p palette) {
	t := c.Theme()
	f := &a.add
	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		modalHeader(c, p, "shield", "添加邮箱", "邮件、授权码都只保存在本机（授权码 AES-256-GCM 加密）。", func() { a.addOpen = false })

		ui.Column(c).Padding(2, 24, 8).Gap(10).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Gap(4).AlignItems(ui.Stretch).Children(func() {
				ui.Text(c, "邮箱服务商").FontSize(12.5).FontWeight(500).TextColor(p.TextSoft)
				ui.Select(c, &f.Provider, a.providerLabels).Label("服务商")
			})

			if h := a.providerHint(); h != "" {
				ui.Row(c).Gap(8).AlignItems(ui.Start).Padding(10, 12).Radius(10).
					Background(p.FieldGroupBg).Border(1, p.SubtleDivider).Children(func() {
					ui.Icon(c, icon("mail")).FontSize(14).Shrink(0).TextColor(t.Accent).Margin(2, 0, 0, 0)
					ui.Text(c, h).FontSize(12).LineHeight(1.45).TextColor(t.TextMuted).Grow(1)
				})
			}

			ui.Column(c).Gap(8).AlignItems(ui.Stretch).Children(func() {
				ui.Text(c, "账号与授权凭证").FontSize(12.5).FontWeight(500).TextColor(p.TextSoft)
				if ui.TextInput(c, &f.Email).Placeholder("邮箱地址（如 name@qq.com）").Label("邮箱地址").AutoFocus().Changed() {
					a.GuessFromEmail()
				}
				ui.TextInput(c, &f.Password).Password().Placeholder("授权码 / 应用专用密码").Label("授权码")
				ui.TextInput(c, &f.Label).Placeholder("显示名称（可选，如「工作邮箱」）").Label("显示名称")
			})

			if a.providerID(f.Provider) == "custom" {
				ui.Column(c).Gap(6).Padding(10, 12).Radius(10).Background(p.FieldGroupBg).Border(1, p.SubtleDivider).Children(func() {
					ui.Text(c, "自定义服务器参数").FontSize(12).FontWeight(500).TextColor(p.TextSoft)
					ui.Row(c).Gap(8).Children(func() {
						ui.TextInput(c, &f.IMAPHost).Placeholder("IMAP 服务器").Label("IMAP 服务器").Grow(1)
						ui.TextInput(c, &f.IMAPPort).Width(78).Label("IMAP 端口")
					})
					ui.Row(c).Gap(8).Children(func() {
						ui.TextInput(c, &f.SMTPHost).Placeholder("SMTP 服务器").Label("SMTP 服务器").Grow(1)
						ui.TextInput(c, &f.SMTPPort).Width(78).Label("SMTP 端口")
					})
				})
			}

			if f.Err != "" {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10, 12).Radius(10).
					Background(t.Danger.Alpha(0.10)).Border(1, t.Danger.Alpha(0.28)).Children(func() {
					ui.Text(c, f.Err).FontSize(12.5).TextColor(t.Danger).Grow(1).Selectable()
				})
			}
		})

		ui.Row(c).Gap(8).Justify(ui.End).Padding(14, 24, 20).Children(func() {
			pressable(c, ui.Button(c, "取消")).OnClick(func() { a.addOpen = false })
			label := "连接并添加"
			if f.Busy {
				label = "正在验证…"
			}
			pressable(c, ui.PrimaryButton(c, label).Disabled(f.Busy)).OnClick(a.SubmitAdd)
		})
	})
}

/* ———— 设置 ———— */

func (a *App) settingsView(c *ui.Context, p palette) {
	t := c.Theme()
	s := &a.settings
	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		modalHeader(c, p, "settings", "偏好设置", "定制外观、列表密度与本地 AI 摘要助手", func() {
			a.settingsOpen = false
			a.saveSettings()
		})

		ui.Scroll(c).MaxHeight(520).Children(func() {
			ui.Column(c).Padding(2, 24, 8).Gap(14).AlignItems(ui.Stretch).Children(func() {
				// 分组一：外观与提醒
				ui.Column(c).Radius(12).Background(p.FieldGroupBg).Border(1, p.SubtleDivider).Children(func() {
					settingRow := func(label, desc string, divider bool, fn func()) {
						ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(11, 16).Children(func() {
							ui.Column(c).Grow(1).Gap(1).Children(func() {
								ui.Text(c, label).FontSize(13.5).FontWeight(500)
								if desc != "" {
									ui.Text(c, desc).FontSize(11.5).TextColor(t.TextMuted)
								}
							})
							fn()
						})
						if divider {
							ui.Box(c).Height(1).Background(p.SubtleDivider)
						}
					}

					themes := []string{"system", "light", "dark"}
					ti := max(0, indexOf(themes, s.Theme))
					settingRow("界面外观", "浅色纸墨、深色烛光或跟随系统", true, func() {
						if ui.Segmented(c, &ti, "跟随系统", "浅色", "深色").Changed() {
							s.Theme = themes[ti]
							a.emitTheme() // 窗口边框与标题栏随主题即时切换
						}
					})

					di := 0
					if s.Density == "compact" {
						di = 1
					}
					settingRow("列表密度", "舒适模式展示两行预览与智能分类标签", true, func() {
						if ui.Segmented(c, &di, "舒适", "紧凑").Changed() {
							s.Density = []string{"cozy", "compact"}[di]
						}
					})

					settingRow("新邮件系统通知", "后台收到新信件时推送桌面提醒", true, func() {
						ui.Switch(c, &s.Notifications).Label("新邮件通知")
					})
					settingRow("仅通知重要邮件", "过滤订阅与通知类邮件的弹窗打扰", false, func() {
						ui.Switch(c, &s.ImportantOnly).Label("仅重要邮件")
					})
				})

				// 分组二：AI 助手
				ui.Column(c).Padding(14, 16).Gap(10).Radius(12).Background(p.FieldGroupBg).Border(1, p.SubtleDivider).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icon("sparkles")).FontSize(15).TextColor(t.Accent)
						ui.Text(c, "AI 邮件助手").FontSize(14).FontWeight(600)
						ui.Text(c, "· OpenAI 兼容接口（智谱 GLM / DeepSeek / Ollama）").FontSize(12).TextColor(t.TextMuted)
					})
					inputRow := func(label string, fn func()) {
						ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
							ui.Text(c, label).Width(76).FontSize(13).TextColor(p.TextSoft)
							fn()
						})
					}
					inputRow("API 地址", func() { ui.TextInput(c, &s.AIBaseURL).Grow(1).Label("API 地址") })
					inputRow("API Key", func() { ui.TextInput(c, &s.AIKey).Password().Placeholder("填入密钥后启用 AI 摘要").Grow(1).Label("API Key") })
					inputRow("模型名称", func() { ui.TextInput(c, &s.AIModel).Grow(1).Label("模型") })
					inputRow("摘要语言", func() { ui.TextInput(c, &s.AILanguage).Grow(1).Label("摘要语言") })
				})

				// 分组三：备份与还原（后端支持时展示）
				if a.backuper() != nil {
					ui.Column(c).Padding(14, 16).Gap(10).Radius(12).Background(p.FieldGroupBg).Border(1, p.SubtleDivider).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, icon("archive")).FontSize(15).TextColor(t.Accent)
							ui.Text(c, "备份与还原").FontSize(14).FontWeight(600)
							ui.Text(c, "· 密码加密的 .velabak 文件").FontSize(12).TextColor(t.TextMuted)
						})
						ui.Text(c, "把邮箱账户（含授权码）、同步进度、本地邮件与偏好设置打包成一个加密文件，换电脑时一键还原。").
							FontSize(12).LineHeight(1.5).TextColor(t.TextMuted)
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							pressable(c, ui.Button(c, "导出备份…")).OnClick(a.StartBackupExport)
							pressable(c, ui.Button(c, "从备份还原…")).OnClick(a.StartBackupImport)
						})
						if where := a.secretWhere(); where != "" {
							ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
								ui.Icon(c, icon("shield")).FontSize(12).TextColor(t.TextMuted)
								ui.Text(c, where).FontSize(11.5).TextColor(t.TextMuted)
							})
						}
					})
				}
			})
		})

		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(14, 24, 20).Children(func() {
			pressable(c, ui.Button(c, "测试 AI")).Disabled(!a.AIReady()).OnClick(a.TestAI)
			ui.Spacer(c)
			pressable(c, ui.PrimaryButton(c, "完成")).OnClick(func() { a.settingsOpen = false; a.saveSettings(); a.emitTheme() })
		})
	})
	if !a.settingsOpen {
		a.saveSettings()
		a.emitTheme()
	}
}

func indexOf(l []string, s string) int {
	for i, x := range l {
		if x == s {
			return i
		}
	}
	return -1
}

/* ———— 搜索与命令面板（原 CommandPalette）———— */

func (a *App) paletteView(c *ui.Context, p palette) {
	t := c.Theme()
	q := strings.ToLower(strings.TrimSpace(a.paletteQ))
	var hits []*core.Email
	if q != "" {
		for i := range a.emails {
			if matches(&a.emails[i], q) {
				hits = append(hits, &a.emails[i])
				if len(hits) == 30 {
					break
				}
			}
		}
	}

	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		// 顶部大号聚焦搜索栏
		var in ui.Element
		ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(16, 18).Children(func() {
			ui.Icon(c, icon("search")).FontSize(18).Shrink(0).TextColor(t.Accent)
			in = ui.TextInputBase(c, &a.paletteQ).Grow(1).FontSize(15).
				Placeholder("搜索邮件，或输入命令：写邮件 / 同步 / 设置…").Label("搜索邮件").AutoFocus().FocusRing(false)
			ui.Box(c).Padding(2, 7).Radius(5).Background(p.KeycapBg).Children(func() {
				ui.Text(c, "ESC").FontSize(10.5).FontWeight(600).TextColor(t.TextMuted).SingleLine()
			})
		})
		ui.Box(c).Height(1).Background(p.SubtleDivider)

		cmds := []struct {
			icon     string
			label    string
			shortcut string
			run      func()
		}{
			{"square-pen", "写邮件", shortcut("N"), func() { a.StartCompose("new") }},
			{"refresh-cw", "立即同步", shortcut("R"), a.Sync},
			{"plus", "添加邮箱", "", func() { a.addOpen = true }},
			{"settings", "设置", "", func() { a.settingsOpen = true }},
		}
		if a.backuper() != nil {
			cmds = append(cmds,
				struct{ icon, label, shortcut string; run func() }{"archive", "导出备份", "", a.StartBackupExport},
				struct{ icon, label, shortcut string; run func() }{"archive", "从备份还原", "", a.StartBackupImport})
		}

		var visibleCmds []int
		for i, cmd := range cmds {
			if q == "" || strings.Contains(strings.ToLower(cmd.label), q) {
				visibleCmds = append(visibleCmds, i)
			}
		}

		ui.Scroll(c).MaxHeight(390).Children(func() {
			ui.Column(c).Padding(8, 10, 12).Gap(2).AlignItems(ui.Stretch).Children(func() {
				if len(visibleCmds) > 0 {
					ui.Text(c, "快捷命令").FontSize(11).FontWeight(600).TextColor(t.TextMuted).Padding(4, 10, 4)
					for _, idx := range visibleCmds {
						cmd := cmds[idx]
						run := cmd.run
						r := ui.Row(c.Key("cmd-" + cmd.label)).Gap(10).AlignItems(ui.Center).
							Padding(8, 10).Radius(9).Role(ui.RoleButton).Label(cmd.label)
						r.Children(func() {
							ui.Box(c).Size(26, 26).Radius(7).Center().Background(p.HoverWash).Children(func() {
								ui.Icon(c, icon(cmd.icon)).FontSize(14).TextColor(p.TextSoft)
							})
							ui.Text(c, cmd.label).FontSize(13.5).Grow(1)
							if cmd.shortcut != "" {
								ui.Box(c).Padding(2, 6).Radius(5).Background(p.KeycapBg).Children(func() {
									ui.Text(c, cmd.shortcut).FontSize(11).TextColor(t.TextMuted).SingleLine()
								})
							}
						})
						hoverBG(c, p, r, false)
						r.OnClick(func() { a.searchOpen = false; run() })
					}
				}

				if len(hits) > 0 {
					ui.Text(c, fmt.Sprintf("匹配邮件（%d）", len(hits))).FontSize(11).FontWeight(600).
						TextColor(t.TextMuted).Padding(10, 10, 4)
					for _, e := range hits {
						id := e.ID
						r := ui.Row(c.Key("hit-" + id)).Gap(10).AlignItems(ui.Center).
							Padding(8, 10).Radius(9).Role(ui.RoleButton).Label(e.Subject)
						r.Children(func() {
							senderAvatar(c, e.From.Name, e.From.Email, 28, t.Dark)
							ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
								ui.Text(c, nonEmpty(e.Subject, "（无主题）")).FontSize(13.5).FontWeight(500).MaxLines(1).Ellipsis("…")
								ui.Text(c, nonEmpty(e.From.Name, e.From.Email)+" · "+shortDate(e.Date)).FontSize(12).TextColor(t.TextMuted)
							})
						})
						hoverBG(c, p, r, false)
						r.OnClick(func() {
							a.searchOpen = false
							if _, x := a.find(id); x != nil {
								a.folder = x.Folder
								a.Select(id)
							}
						})
					}
				} else if q != "" && len(visibleCmds) == 0 {
					ui.Text(c, "没有找到匹配的邮件或命令").FontSize(13).TextColor(t.TextMuted).
						Padding(28, 12).TextAlign(ui.Center)
				}
			})
		})

		if in.Submitted() && len(hits) > 0 {
			a.searchOpen = false
			a.folder = hits[0].Folder
			a.Select(hits[0].ID)
		}
	})
}
