package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo/ui"
)

// View 构建整个窗口：侧边栏 | 邮件列表 | 阅读区，外加写信、添加邮箱、设置、搜索面板。
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

	for _, m := range a.toasts {
		c.Toast(m)
	}
	a.toasts = a.toasts[:0]

	a.shortcuts(c)

	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		a.sidebar(c)
		ui.Box(c).Width(1).Background(t.Border)
		a.mailList(c)
		ui.Box(c).Width(1).Background(t.Border)
		a.viewer(c)
	})

	ui.Modal(c, &a.composeOpen, func() { a.composeView(c) })
	ui.Modal(c, &a.addOpen, func() { a.addView(c) })
	ui.Modal(c, &a.settingsOpen, func() { a.settingsView(c) })
	ui.Modal(c, &a.searchOpen, func() { a.paletteView(c) })
	if !a.composeOpen && a.compose != nil && !a.compose.Sending {
		a.compose = nil
	}
}

func (a *App) shortcuts(c *ui.Context) {
	if a.composeOpen || a.addOpen || a.settingsOpen || a.searchOpen {
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

/* ———— 小部件 ———— */

func iconButton(c *ui.Context, name, label string) ui.Element {
	b := ui.Button(c, "").Label(label).Tooltip(label).Padding(6).Children(func() {
		ui.Icon(c, icon(name)).FontSize(16)
	})
	return pressable(c, b)
}

func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Width(240).Shrink(0).Padding(14, 10).Gap(4).Background(t.Surface).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 8, 12).Children(func() {
			ui.Icon(c, icon("sailboat")).FontSize(22).TextColor(t.Accent)
			ui.Text(c, "Vela 轻帆").FontSize(18).Bold().Grow(1)
			iconButton(c, "settings", "设置").OnClick(func() { a.settingsOpen = true })
		})
		pressable(c, ui.PrimaryButton(c, "").Label("写邮件").Children(func() {
			ui.Icon(c, icon("square-pen"))
			ui.Text(c, "写邮件").SingleLine()
		}).OnClick(func() { a.StartCompose("new") })).Margin(0, 0, 10)

		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				for _, f := range Folders {
					a.navItem(c, f.ID, f.Label, folderIcon(f.ID), a.Unread(f.ID))
				}
				ui.Text(c, "智能分类").FontSize(12).TextColor(t.TextMuted).Padding(14, 10, 4)
				for _, f := range Categories {
					a.navItem(c, f.ID, f.Label, "sparkles", 0)
				}
			})
		})
		ui.Row(c).AlignItems(ui.Center).Padding(8, 8, 4).Children(func() {
			ui.Text(c, "邮箱账户").FontSize(12).TextColor(t.TextMuted).Grow(1)
			iconButton(c, "plus", "添加邮箱").OnClick(func() { a.addOpen = true })
		})
		a.accountItem(c, "", "全部账户")
		for _, acc := range a.accounts {
			a.accountItem(c, acc.ID, acc.Label)
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(10, 8, 0).Children(func() {
			label := "未同步"
			if a.syncing {
				label = "同步中…"
			} else if !a.syncedAt.IsZero() {
				label = "同步于 " + a.syncedAt.Format("15:04")
			}
			ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).Grow(1)
			iconButton(c, "refresh-cw", "立即同步").Disabled(a.syncing || len(a.accounts) == 0).OnClick(a.Sync)
		})
	})
}

func folderIcon(id string) string {
	return map[string]string{"inbox": "inbox", "starred": "star", "sent": "send", "drafts": "file-text", "archive": "archive", "trash": "trash-2"}[id]
}

func (a *App) navItem(c *ui.Context, id, label, ic string, count int) {
	t := c.Theme()
	on := a.folder == id
	row := ui.Row(c.Key("nav-" + id)).Gap(10).AlignItems(ui.Center).Padding(7, 10).Radius(t.Radius).Role(ui.RoleButton).Label(label)
	row.Children(func() {
		ui.Icon(c, icon(ic)).FontSize(16)
		ui.Text(c, label).Grow(1)
		if count > 0 {
			ui.Text(c, fmt.Sprint(count)).FontSize(12).TextColor(t.TextMuted)
		}
	})
	hoverBG(c, row, on)
	if on {
		row.TextColor(t.Accent)
	}
	row.OnClick(func() { a.SetFolder(id) })
}

func (a *App) accountItem(c *ui.Context, id, label string) {
	t := c.Theme()
	on := a.account == id
	row := ui.Row(c.Key("acc-"+id)).Gap(8).AlignItems(ui.Center).Padding(6, 10).Radius(t.Radius).Role(ui.RoleButton).Label(label)
	row.Children(func() {
		ui.Icon(c, icon("mail")).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, label).FontSize(13).Grow(1).MaxLines(1).Ellipsis("…")
	})
	hoverBG(c, row, on)
	row.OnClick(func() { a.account, a.selected = id, "" })
	if id != "" {
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("移除此账户").Chosen() {
				a.RemoveAccount(id)
			}
		})
	}
}

func hoverBG(c *ui.Context, e ui.Element, on bool) {
	t := c.Theme()
	switch {
	case on:
		e.Background(t.Selection)
	case e.Pressed():
		e.Background(t.SurfacePressed)
	case e.Hovered():
		e.Background(t.SurfaceHover)
	}
	colorFade(c, e)
}

/* ———— 邮件列表（虚拟列表：只构建可见行）———— */

func (a *App) folderLabel() string {
	for _, f := range append(Folders, Categories...) {
		if f.ID == a.folder {
			return f.Label
		}
	}
	return a.folder
}

func (a *App) mailList(c *ui.Context) {
	t := c.Theme()
	items := a.Visible()
	ui.Column(c).Width(400).Shrink(0).AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Padding(16, 16, 10).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.folderLabel()).FontSize(20).Bold().Grow(1)
			ui.Text(c, fmt.Sprintf("%d 封", len(items))).FontSize(12).TextColor(t.TextMuted)
		})
		ui.Row(c).Padding(0, 16, 10).Children(func() {
			ui.SearchField(c, &a.query).Placeholder("搜索主题、发件人、正文").Label("搜索").Grow(1)
		})
		ui.Box(c).Height(1).Background(t.Border)
		listBox := ui.Column(c.Key("list-" + a.folder + a.account)).Grow(1).AlignItems(ui.Stretch)
		folderSwitch(c, listBox)
		listBox.Children(func() {
			state := &ui.ListState{
				Key:   func(i int) any { return items[i].ID },
				Label: func(i int) string { return items[i].Subject },
			}
			ui.List(c, state, len(items), func(i int) { a.mailRow(c, items[i]) }).Grow(1).Children(func() {
				if len(items) == 0 {
					msg := "这里空空如也"
					if len(a.accounts) == 0 {
						msg = "还没有邮箱账户，点左下角 + 添加"
					}
					ui.Text(c, msg).TextColor(t.TextMuted).Padding(40, 16).TextAlign(ui.Center)
				}
			})
		})
	})
}

func (a *App) mailRow(c *ui.Context, e *core.Email) {
	t := c.Theme()
	compact := a.settings.Density == "compact"
	pad := float32(12)
	if compact {
		pad = 7
	}
	on := a.selected == e.ID
	id := e.ID
	row := ui.Column(c).Padding(pad, 16).Gap(3).Role(ui.RoleButton).Label(e.Subject)
	row.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if !e.Read {
				ui.Box(c).Size(7, 7).Radius(4).Background(t.Accent)
			}
			from := ui.Text(c, e.From.Name).Grow(1).MaxLines(1).Ellipsis("…")
			if !e.Read {
				from.Bold()
			}
			if e.Starred {
				ui.Icon(c, icon("star")).FontSize(12).TextColor(starColor[t.Dark])
			}
			if len(e.Attachments) > 0 {
				ui.Icon(c, icon("paperclip")).FontSize(12).TextColor(t.TextMuted)
			}
			ui.Text(c, shortDate(e.Date)).FontSize(12).TextColor(t.TextMuted)
		})
		subj := ui.Text(c, nonEmpty(e.Subject, "（无主题）")).MaxLines(1).Ellipsis("…")
		if !e.Read {
			subj.FontWeight(600)
		}
		if !compact {
			preview := e.AISummary
			if preview == "" {
				preview = strings.Join(strings.Fields(e.Body), " ")
			}
			ui.Text(c, preview).FontSize(13).TextColor(t.TextMuted).MaxLines(2).Ellipsis("…")
		}
	})
	hoverBG(c, row, on)
	a.arrivalHighlight(c, row, e.ID, on)
	row.OnClick(func() { a.Select(id) })
	row.ContextMenu(func(m *ui.Menu) {
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

/* ———— 阅读区 ———— */

func (a *App) viewer(c *ui.Context) {
	t := c.Theme()
	e := a.Selected()
	ui.Column(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
		if e == nil {
			ui.Column(c.Key("empty")).Grow(1).Center().Gap(10).Children(func() {
				ui.Icon(c, icon("sailboat")).FontSize(48).TextColor(t.Border)
				ui.Text(c, "选择一封邮件开始阅读").TextColor(t.TextMuted)
				ui.Text(c, "⌘K 搜索 · ⌘N 写邮件 · ⌘R 同步").FontSize(12).TextColor(t.TextMuted)
			})
			return
		}
		pane := ui.Column(c.Key("mail-" + e.ID)).Grow(1).AlignItems(ui.Stretch)
		detailEnter(c, pane)
		pane.Children(func() { a.detail(c, e) })
	})
}

func (a *App) detail(c *ui.Context, e *core.Email) {
	t := c.Theme()
	id := e.ID
	ui.Row(c).Padding(10, 16).Gap(4).AlignItems(ui.Center).Children(func() {
		iconButton(c, "reply", "回复").OnClick(func() { a.StartCompose("reply") })
		iconButton(c, "reply-all", "回复全部").OnClick(func() { a.StartCompose("replyAll") })
		iconButton(c, "forward", "转发").OnClick(func() { a.StartCompose("forward") })
		ui.Box(c).Width(1).Height(18).Background(t.Border).Margin(0, 6)
		iconButton(c, "archive", "归档此邮件").OnClick(func() { a.Move(id, "archive") })
		iconButton(c, "trash-2", "删除此邮件").OnClick(func() { a.Move(id, "trash") })
		iconButton(c, map[bool]string{true: "star-off", false: "star"}[e.Starred], map[bool]string{true: "取消星标", false: "加星标"}[e.Starred]).OnClick(func() { a.ToggleStar(id) })
		iconButton(c, "mail-open", map[bool]string{true: "标为未读", false: "标为已读"}[e.Read]).OnClick(func() { a.ToggleRead(id) })
		ui.Spacer(c)
		label := "AI 分析"
		if a.aiLoading == id {
			label = "分析中…"
		}
		pressable(c, ui.Button(c, "").Label(label).Disabled(a.aiLoading != "").Children(func() {
			ui.Icon(c, icon("sparkles"))
			ui.Text(c, label).SingleLine()
		}).OnClick(func() { a.Analyze(id) }))
	})
	ui.Box(c).Height(1).Background(t.Border)
	ui.Column(c).Padding(20, 28, 12).Gap(8).Children(func() {
		ui.Text(c, nonEmpty(e.Subject, "（无主题）")).FontSize(22).Bold().Selectable()
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Avatar(c, e.From.Name, nil).Size(34, 34)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, e.From.Name+"  <"+e.From.Email+">").FontWeight(600).Selectable()
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
					line += "  抄送：" + strings.Join(cc, ", ")
				}
				ui.Text(c, line).FontSize(12).TextColor(t.TextMuted).MaxLines(2).Ellipsis("…")
			})
			if d, err := time.Parse(time.RFC3339, e.Date); err == nil {
				ui.Text(c, d.Local().Format("2006年1月2日 15:04")).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if e.AISummary != "" {
			ui.Column(c).Padding(10, 12).Gap(4).Radius(t.Radius).Background(t.Selection).Children(func() {
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
				ui.Text(c, e.AISummary).Selectable()
				if e.AIWhy != "" {
					ui.Text(c, e.AIWhy).FontSize(12).TextColor(t.TextMuted)
				}
			})
		}
		if len(e.Attachments) > 0 {
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for _, at := range e.Attachments {
					ui.Row(c.Key(at.ID)).Gap(6).AlignItems(ui.Center).Padding(5, 10).Radius(t.Radius).Border(1, t.Border).Children(func() {
						ui.Icon(c, icon("paperclip")).FontSize(13)
						ui.Text(c, at.Name).FontSize(13)
						ui.Text(c, formatSize(at.SizeKB)).FontSize(12).TextColor(t.TextMuted)
					}).Tooltip("附件内容未下载（与原版相同，只同步附件信息）")
				}
			})
		}
	})
	if e.HTMLBody != "" && a.body != nil {
		a.body.Show(e.ID, e.HTMLBody)
		ui.WebView(c, a.body.Native()).Grow(1).Margin(0, 16, 16).Radius(t.Radius).Border(1, t.Border)
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Padding(4, 28, 28).Gap(12).Children(func() {
			for _, p := range strings.Split(strings.TrimSpace(e.Body), "\n\n") {
				ui.Text(c, p).LineHeight(1.6).Selectable()
			}
		})
	})
}

func formatSize(kb int) string {
	if kb >= 1024 {
		return fmt.Sprintf("%.1f MB", float64(kb)/1024)
	}
	return fmt.Sprintf("%d KB", max(kb, 1))
}

/* ———— 写信 ———— */

var composeTitle = map[string]string{"new": "写邮件", "reply": "回复", "replyAll": "回复全部", "forward": "转发"}

func (a *App) composeView(c *ui.Context) {
	t := c.Theme()
	cp := a.compose
	if cp == nil {
		return
	}
	box := ui.Column(c).Width(640).Gap(10)
	dialogEnter(c, box)
	box.Children(func() {
		ui.Text(c, composeTitle[cp.Mode]).FontSize(18).Bold()
		if len(a.accounts) > 1 {
			var ids []string
			for _, acc := range a.accounts {
				ids = append(ids, acc.ID)
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "发件人").Width(56).TextColor(t.TextMuted)
				ui.Select(c, &cp.AccountID, ids).Grow(1).Label("发件人")
			})
		}
		field := func(label string, v *string, focus bool) {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, label).Width(56).TextColor(t.TextMuted)
				in := ui.TextInput(c, v).Grow(1).Label(label)
				if focus {
					in.AutoFocus()
				}
			})
		}
		field("收件人", &cp.To, cp.Mode == "new" || cp.Mode == "forward")
		field("抄送", &cp.CC, false)
		field("主题", &cp.Subject, false)
		ta := ui.TextArea(c, &cp.Body).Height(300).Label("正文").Placeholder("正文")
		if cp.Mode == "reply" || cp.Mode == "replyAll" {
			ta.AutoFocus()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			pressable(c, ui.Button(c, "存草稿")).OnClick(a.SaveDraft)
			pressable(c, ui.Button(c, "取消")).OnClick(func() { a.composeOpen = false })
			label := "发送"
			if cp.Sending {
				label = "发送中…"
			}
			pressable(c, ui.PrimaryButton(c, label).Disabled(cp.Sending)).OnClick(a.SendCompose)
		})
	})
}

/* ———— 添加邮箱 ———— */

func (a *App) addView(c *ui.Context) {
	t := c.Theme()
	f := &a.add
	box := ui.Column(c).Width(480).Gap(10)
	dialogEnter(c, box)
	box.Children(func() {
		ui.Text(c, "添加邮箱").FontSize(18).Bold()
		ui.Text(c, "邮件、授权码都只保存在本机（授权码 AES-256-GCM 加密）。").FontSize(12).TextColor(t.TextMuted)
		ui.Select(c, &f.Provider, a.providerLabels).Label("服务商")
		if h := a.providerHint(); h != "" {
			ui.Text(c, h).FontSize(12).TextColor(t.TextMuted)
		}
		if ui.TextInput(c, &f.Email).Placeholder("邮箱地址").Label("邮箱地址").AutoFocus().Changed() {
			a.GuessFromEmail()
		}
		ui.TextInput(c, &f.Password).Password().Placeholder("授权码 / 应用专用密码").Label("授权码")
		ui.TextInput(c, &f.Label).Placeholder("显示名称（可选）").Label("显示名称")
		if a.providerID(f.Provider) == "custom" {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &f.IMAPHost).Placeholder("IMAP 服务器").Label("IMAP 服务器").Grow(1)
				ui.TextInput(c, &f.IMAPPort).Width(70).Label("IMAP 端口")
			})
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &f.SMTPHost).Placeholder("SMTP 服务器").Label("SMTP 服务器").Grow(1)
				ui.TextInput(c, &f.SMTPPort).Width(70).Label("SMTP 端口")
			})
		}
		if f.Err != "" {
			ui.Text(c, f.Err).FontSize(13).TextColor(t.Danger).Selectable()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
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

func (a *App) settingsView(c *ui.Context) {
	t := c.Theme()
	s := &a.settings
	box := ui.Column(c).Width(520).Gap(12)
	dialogEnter(c, box)
	box.Children(func() {
		ui.Text(c, "设置").FontSize(18).Bold()
		row := func(label string, fn func()) {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, label).Width(110).TextColor(t.TextMuted)
				fn()
			})
		}
		themes := []string{"system", "light", "dark"}
		ti := max(0, indexOf(themes, s.Theme))
		row("外观", func() {
			if ui.Segmented(c, &ti, "跟随系统", "浅色", "深色").Changed() {
				s.Theme = themes[ti]
			}
		})
		di := 0
		if s.Density == "compact" {
			di = 1
		}
		row("列表密度", func() {
			if ui.Segmented(c, &di, "舒适", "紧凑").Changed() {
				s.Density = []string{"cozy", "compact"}[di]
			}
		})
		row("新邮件通知", func() { ui.Switch(c, &s.Notifications).Label("新邮件通知") })
		row("仅重要邮件", func() { ui.Switch(c, &s.ImportantOnly).Label("仅重要邮件") })
		ui.Box(c).Height(1).Background(t.Border)
		ui.Text(c, "AI（OpenAI 兼容接口，如智谱 GLM / DeepSeek / OpenAI / Ollama）").FontWeight(600)
		row("API 地址", func() { ui.TextInput(c, &s.AIBaseURL).Grow(1).Label("API 地址") })
		row("API Key", func() { ui.TextInput(c, &s.AIKey).Password().Grow(1).Label("API Key") })
		row("模型", func() { ui.TextInput(c, &s.AIModel).Grow(1).Label("模型") })
		row("摘要语言", func() { ui.TextInput(c, &s.AILanguage).Grow(1).Label("摘要语言") })
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			pressable(c, ui.Button(c, "测试 AI")).Disabled(!a.AIReady()).OnClick(a.TestAI)
			pressable(c, ui.PrimaryButton(c, "完成")).OnClick(func() { a.settingsOpen = false; a.saveSettings() })
		})
	})
	if !a.settingsOpen {
		a.saveSettings()
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

/* ———— 搜索面板（原 CommandPalette）———— */

func (a *App) paletteView(c *ui.Context) {
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
	box := ui.Column(c).Width(560).Gap(8)
	dialogEnter(c, box)
	box.Children(func() {
		in := ui.SearchField(c, &a.paletteQ).Placeholder("搜索邮件，或输入命令：写邮件 / 同步 / 设置").Label("搜索邮件").AutoFocus()
		cmds := []struct{ label string; run func() }{
			{"写邮件", func() { a.StartCompose("new") }},
			{"立即同步", a.Sync},
			{"添加邮箱", func() { a.addOpen = true }},
			{"设置", func() { a.settingsOpen = true }},
		}
		ui.Column(c).Gap(2).MaxHeight(380).Children(func() {
			for _, cmd := range cmds {
				if q != "" && !strings.Contains(cmd.label, q) {
					continue
				}
				run := cmd.run
				r := ui.Row(c.Key("cmd-"+cmd.label)).Padding(8, 10).Radius(t.Radius).Role(ui.RoleButton).Children(func() {
					ui.Text(c, "› "+cmd.label)
				})
				hoverBG(c, r, false)
				r.OnClick(func() { a.searchOpen = false; run() })
			}
			for _, e := range hits {
				id := e.ID
				r := ui.Column(c.Key("hit-"+id)).Padding(8, 10).Gap(2).Radius(t.Radius).Role(ui.RoleButton).Label(e.Subject).Children(func() {
					ui.Text(c, nonEmpty(e.Subject, "（无主题）")).MaxLines(1).Ellipsis("…")
					ui.Text(c, e.From.Name+" · "+shortDate(e.Date)).FontSize(12).TextColor(t.TextMuted)
				})
				hoverBG(c, r, false)
				r.OnClick(func() {
					a.searchOpen = false
					if _, x := a.find(id); x != nil {
						a.folder = x.Folder
						a.Select(id)
					}
				})
			}
		})
		if in.Submitted() && len(hits) > 0 {
			a.searchOpen = false
			a.folder = hits[0].Folder
			a.Select(hits[0].ID)
		}
	})
}
