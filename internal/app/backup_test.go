package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azzliang6/vela/internal/core"
)

// fakeBackup 在 fakeBackend 之上补上备份能力，记录调用并按剧本返回。
type fakeBackup struct {
	fakeBackend
	exportPass string
	importPass string
	importRes  *core.RestoreResult
	importErr  error
	calls      int
}

func (f *fakeBackup) ExportBackup(pass string, settings json.RawMessage) ([]byte, error) {
	f.calls++
	f.exportPass = pass
	return []byte("VELABAK-fake"), nil
}

func (f *fakeBackup) ImportBackup(pass string, data []byte) (*core.RestoreResult, error) {
	f.calls++
	f.importPass = pass
	return f.importRes, f.importErr
}

func newBackupApp(t *testing.T) (*App, *fakeBackup, string) {
	t.Helper()
	dir := t.TempDir()
	f := &fakeBackup{
		fakeBackend: fakeBackend{
			accounts: []core.MailboxAccount{{ID: "me@qq.com", Email: "me@qq.com", Label: "我的 QQ"}},
			emails:   []core.Email{mail("1", "周会纪要", "Alice", true)},
			sync:     map[string]core.SyncOutcome{},
		},
		importRes: &core.RestoreResult{Accounts: 2, Emails: 9, Settings: json.RawMessage(`{"theme":"dark"}`),
			Snapshot: filepath.Join(dir, "snapshots", "vela-before-restore-20261123-101010.db")},
	}
	a := New(f, "")
	a.SetDialogs(FileDialogs{
		SaveFile: func(string, string) (string, bool) {
			return filepath.Join(dir, "out.velabak"), true
		},
		OpenFile: func(string) (string, bool) {
			p := filepath.Join(dir, "in.velabak")
			_ = os.WriteFile(p, []byte("VELABAK-fake"), 0o600) // 「选中」的备份文件真实存在
			return p, true
		},
	})
	return a, f, dir
}

// finishRestore 走一遍完整的还原流程：打开弹窗、选文件、输入密码、提交。
func finishRestore(a *App) {
	a.StartBackupImport()
	a.pickBackupFile()
	a.backupPass = "口令1234"
	a.SubmitBackup()
}

func TestBackupExportFlow(t *testing.T) {
	a, f, dir := newBackupApp(t)
	a.StartBackupExport()
	if !a.backupOpen || a.backupMode != "export" {
		t.Fatal("应打开导出弹窗")
	}
	a.backupPass, a.backupPass2 = "口令1234", "口令1234"
	a.SubmitBackup()
	if f.calls != 1 || f.exportPass != "口令1234" {
		t.Fatalf("应调用导出一次：%d %q", f.calls, f.exportPass)
	}
	b, err := os.ReadFile(filepath.Join(dir, "out.velabak"))
	if err != nil || string(b) != "VELABAK-fake" {
		t.Fatalf("备份文件未落盘：%v %q", err, b)
	}
	if a.backupOpen {
		t.Error("成功后应关闭弹窗")
	}
	if len(a.toasts) != 1 || !strings.Contains(a.toasts[0], "备份已保存") {
		t.Fatalf("应有成功提示：%v", a.toasts)
	}
}

func TestBackupExportValidation(t *testing.T) {
	a, f, _ := newBackupApp(t)
	a.StartBackupExport()

	a.backupPass, a.backupPass2 = "口令1234", "不一致"
	a.SubmitBackup()
	if a.backupErr == "" || !strings.Contains(a.backupErr, "不一致") {
		t.Fatalf("两次密码不一致应报错：%q", a.backupErr)
	}
	if f.calls != 0 {
		t.Fatal("校验失败不应调用导出")
	}

	a.backupErr = ""
	a.backupPass, a.backupPass2 = "口令1234", "口令1234"
	a.dialogs.SaveFile = func(string, string) (string, bool) { return "", false }
	a.SubmitBackup()
	if !a.backupOpen || f.calls != 0 {
		t.Fatal("用户取消保存对话框时应留在弹窗、不调用导出")
	}
}

func TestBackupImportFlow(t *testing.T) {
	a, f, _ := newBackupApp(t)
	a.emails = append(a.emails, mail("5", "还原前的本地邮件", "Old", true))
	a.account, a.selected = "gone@x.com", a.emails[0].ID
	genBefore := a.dataGen

	finishRestore(a)
	if f.calls != 1 || f.importPass != "口令1234" {
		t.Fatalf("应调用还原一次：%d %q", f.calls, f.importPass)
	}
	if a.settings.Theme != "dark" {
		t.Fatalf("还原后应套用备份的主题：%q", a.settings.Theme)
	}
	// 账户与邮件列表整体换成备份里的
	if _, x := a.find("me@qq.com:inbox:5"); x != nil || len(a.emails) != 1 {
		t.Fatalf("还原后列表应换成备份里的邮件：%d", len(a.emails))
	}
	if len(a.accounts) != 1 || a.accounts[0].ID != "me@qq.com" {
		t.Fatalf("还原后账户应重新读取：%+v", a.accounts)
	}
	if a.dataGen != genBefore+1 {
		t.Fatal("还原后应让之前发起的同步结果作废")
	}
	// 正在筛选的账户已被覆盖掉：回到「全部账户」，否则列表会是空的
	if a.account != "" || a.selected != "" || a.backupOpen {
		t.Fatalf("还原后应关闭弹窗并清空筛选与选中：account=%q selected=%q open=%v", a.account, a.selected, a.backupOpen)
	}
	if len(a.toasts) != 1 || !strings.Contains(a.toasts[0], "还原完成") || !strings.Contains(a.toasts[0], "已留副本") {
		t.Fatalf("应有还原成功并提到副本的提示：%v", a.toasts)
	}
}

func TestRestoreKeepsFilterForExistingAccount(t *testing.T) {
	a, f, _ := newBackupApp(t)
	a.account = "me@qq.com"
	f.importRes.Snapshot = "" // 本机原本没有数据时不会留副本
	finishRestore(a)
	if a.account != "me@qq.com" {
		t.Fatalf("备份里仍有这个账户，筛选不应被重置：%q", a.account)
	}
	if len(a.toasts) != 1 || strings.Contains(a.toasts[0], "副本") {
		t.Fatalf("没有留副本时提示里不应提副本：%v", a.toasts)
	}
}

func TestBackupImportWrongPassKeepsModal(t *testing.T) {
	a, f, _ := newBackupApp(t)
	f.importErr = errorString("密码错误或备份文件已损坏")
	finishRestore(a)
	if !a.backupOpen || a.backupErr == "" || !strings.Contains(a.backupErr, "密码错误") {
		t.Fatalf("还原失败应留在弹窗并提示：%v %q", a.backupOpen, a.backupErr)
	}
	if len(a.toasts) != 0 {
		t.Fatalf("还原失败不应提示成功：%v", a.toasts)
	}
}

func TestRestoreDropsSyncStartedBeforeIt(t *testing.T) {
	a, f, _ := newBackupApp(t)
	late := mail("9", "迟到的同步结果", "Late", false)
	f.sync["me@qq.com"] = core.SyncOutcome{AccountID: "me@qq.com", Emails: []core.Email{late}}

	// 同步在后台进行：结果已经算好，但还没回到主线程
	done := make(chan func(), 1)
	a.post, a.async = func(fn func()) { done <- fn }, true
	a.Sync()
	finishSync := <-done

	a.post, a.async = func(fn func()) { fn() }, false // 还原本身同步执行
	finishRestore(a)                                  // 这期间完成了一次还原
	finishSync()                                      // 针对旧数据的同步结果这时才回来

	if a.syncing {
		t.Fatal("丢弃过期结果后应结束「同步中」状态")
	}
	if _, x := a.find(late.ID); x != nil {
		t.Fatal("还原之前发起的同步针对的是旧数据，结果不应合并回来")
	}
	if len(a.toasts) != 1 {
		t.Fatalf("过期的同步结果不应再提示：%v", a.toasts)
	}
}

func TestNewMailForRemovedAccountIsDropped(t *testing.T) {
	a, _ := newTestApp()
	ghost := mail("7", "来自已删除账户的信", "Ghost", false)
	ghost.AccountID = "ghost@x.com"
	a.OnNewMail("ghost@x.com", []core.Email{ghost})
	if _, x := a.find(ghost.ID); x != nil || len(a.toasts) != 0 {
		t.Fatalf("账户已不存在，它那条连接迟到的邮件不应写回：%v", a.toasts)
	}
	// 还在的账户照常收取
	a.OnNewMail("me@qq.com", []core.Email{mail("8", "新到的邮件", "Bob", false)})
	if _, x := a.find("me@qq.com:inbox:8"); x == nil {
		t.Fatal("现存账户的新邮件应正常收取")
	}
}

func TestBackupNeedsCapableBackend(t *testing.T) {
	a, _ := newTestApp() // fakeBackend 不实现 backupCapable
	a.StartBackupExport()
	if a.backupOpen {
		t.Fatal("后端不支持时不应打开弹窗")
	}
	if len(a.toasts) != 1 {
		t.Fatalf("应提示不支持：%v", a.toasts)
	}
}

// errorString 是固定错误的极简实现。
type errorString string

func (e errorString) Error() string { return string(e) }
