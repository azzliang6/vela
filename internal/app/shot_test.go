package app

import (
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo/ui"
)

// VELA_SHOTS=dir go test -run TestShots 会把主要界面渲染成 PNG，用于人工检查。
func TestShots(t *testing.T) {
	dir := os.Getenv("VELA_SHOTS")
	if dir == "" {
		t.Skip("设置 VELA_SHOTS 才渲染截图")
	}
	save := func(tt *ui.Tester, name string) {
		tt.Advance(320 * time.Millisecond)
		f, _ := os.Create(dir + "/" + name + ".png")
		defer f.Close()
		_ = png.Encode(f, tt.Image())
	}
	a, _ := newTestApp()
	// 丰富截图示例数据：加一封带 AI 摘要、分类与附件的邮件，外加一封未读新信件
	if len(a.emails) >= 2 {
		a.emails[1].Category = "finance"
		a.emails[1].AISummary = "本期招行信用卡账单 ¥1,234.00，最后还款日 10 月 25 日，已开启自动还款。"
		a.emails[1].Attachments = []core.Attachment{{ID: "att1", Name: "2026-10-电子账单.pdf", SizeKB: 428}}
	}
	a.emails = append(a.emails, core.Email{
		ID: "me@qq.com:inbox:3", AccountID: "me@qq.com", Folder: "inbox",
		Subject: "Vela v0.9 桌面端体验评审纪要",
		From:    core.Contact{Name: "张启明", Email: "qiming@hangji.tech"},
		To:      []core.Contact{{Name: "我", Email: "me@qq.com"}},
		Body:    "今天评审确认了纸墨界面的留白与字体节奏，计划下周正式发布。",
		Date:    "2026-10-13T09:41:00+08:00", Read: false, Starred: true, Category: "important",
	})
	tt := ui.NewTester(a.View, 1280, 820)
	save(tt, "empty")

	_ = tt.Click("信用卡账单")
	// 恢复列表里另一封未读，方便同时观察选中态、已读态与未读态
	a.emails[2].Read = false
	tt.Frame()
	save(tt, "main")

	_ = tt.Click("回复")
	save(tt, "compose")
	a.composeOpen = false
	tt.Frame()

	_ = tt.Click("添加邮箱")
	save(tt, "add")
	a.addOpen = false
	tt.Frame()

	_ = tt.Click("设置")
	save(tt, "settings")
	tt.Scroll(640, 400, 0, 2000)
	tt.Advance(320 * time.Millisecond)
	f, _ := os.Create(dir + "/settings-update.png")
	_ = png.Encode(f, tt.Image())
	f.Close()
	tt.Scroll(640, 400, 0, -2000)
	a.settingsOpen = false
	tt.Frame()

	a.searchOpen = true
	a.paletteQ = ""
	tt.Frame()
	save(tt, "palette")
	a.searchOpen = false

	a.settings.Theme = "dark"
	tt.Frame()
	save(tt, "dark")
	a.settings.Theme = "system"

	// 备份与还原：换上支持备份的后端，截设置里的新卡片与导出弹窗
	a.be = &fakeBackup{fakeBackend: fakeBackend{
		accounts: []core.MailboxAccount{{ID: "me@qq.com", Email: "me@qq.com", Label: "我的 QQ"}},
		emails:   a.emails,
	}}
	a.accounts, _ = a.be.ListAccounts()
	tt.Frame()

	_ = tt.Click("设置")
	save(tt, "settings-backup")
	a.settingsOpen = false
	tt.Frame()

	a.StartBackupExport()
	tt.Frame()
	save(tt, "backup-export")
	a.backupOpen = false
	a.StartBackupImport()
	tt.Frame()
	save(tt, "backup-import")
	a.backupOpen = false
}
