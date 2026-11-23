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
	exportData []byte
	importPass string
	importRes  *core.RestoreResult
	importErr  error
	calls      int
}

func (f *fakeBackup) ExportBackup(pass string, settings json.RawMessage) ([]byte, error) {
	f.calls++
	f.exportPass = pass
	f.exportData = []byte("VELABAK-fake")
	return f.exportData, nil
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
		},
		importRes: &core.RestoreResult{Accounts: 2, Emails: 9, SecretChanged: true,
			Secret: "还原后的密钥材料", Settings: json.RawMessage(`{"theme":"dark"}`)},
	}
	a := New(f, "")
	a.SetKeyStore(filepath.Join(dir, "config"))
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
	a, f, dir := newBackupApp(t)
	keyDir := filepath.Join(dir, "config")
	a.StartBackupImport()
	if !a.backupOpen || a.backupMode != "import" {
		t.Fatal("应打开还原弹窗")
	}
	a.pickBackupFile() // 模拟点击「浏览…」选中备份文件
	a.backupPass = "口令1234"
	a.SubmitBackup()
	if f.calls != 1 || f.importPass != "口令1234" {
		t.Fatalf("应调用还原一次：%d %q", f.calls, f.importPass)
	}
	// 偏好套用 + 密钥写回 + 列表刷新
	if a.settings.Theme != "dark" {
		t.Fatalf("还原后应套用备份的主题：%q", a.settings.Theme)
	}
	if b, err := os.ReadFile(filepath.Join(keyDir, "mail.key")); err != nil ||
		string(b) != "还原后的密钥材料" {
		t.Fatalf("mail.key 未写回：%q %v", b, err)
	}
	if len(a.toasts) != 1 || !strings.Contains(a.toasts[0], "还原完成") {
		t.Fatalf("应有还原成功提示：%v", a.toasts)
	}
	if a.backupOpen || a.selected != "" {
		t.Fatal("还原后应关闭弹窗并清空选中")
	}
}

func TestBackupImportWrongPassKeepsModal(t *testing.T) {
	a, f, _ := newBackupApp(t)
	f.importErr = errorString("密码错误或备份文件已损坏")
	a.StartBackupImport()
	a.pickBackupFile()
	a.backupPass = "错的"
	a.SubmitBackup()
	if !a.backupOpen || a.backupErr == "" || !strings.Contains(a.backupErr, "密码错误") {
		t.Fatalf("还原失败应留在弹窗并提示：%v %q", a.backupOpen, a.backupErr)
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
