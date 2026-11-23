package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func backupTestEnv(t *testing.T, sub string) *Backend {
	t.Helper()
	oldIters := backupKDFIters
	backupKDFIters = 4096
	t.Cleanup(func() { backupKDFIters = oldIters })
	// t.TempDir() 在同一个测试里每次返回同一路径，用子目录区分多台“设备”
	dir := filepath.Join(t.TempDir(), sub)
	t.Setenv("VELA_DATA_DIR", dir)
	be, err := OpenBackend()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { be.DB.Close() })
	return be
}

// seedMailbox 放入一个账户（授权码加密存储）、同步游标与两封邮件。
func seedMailbox(t *testing.T, be *Backend, secret string) {
	t.Helper()
	cfg := MailServerConfig{Provider: "qq", Email: "me@qq.com", IMAPHost: "imap.qq.com",
		IMAPPort: 993, IMAPSecurity: SecuritySSL, SMTPHost: "smtp.qq.com", SMTPPort: 465,
		SMTPSecurity: SecuritySSL, Password: secret}
	if _, err := be.DB.UpsertAccount(cfg, "我的 QQ", be.Crypto()); err != nil {
		t.Fatal(err)
	}
	bf := uint32(500)
	if err := be.DB.SaveCursors("me@qq.com", []MailboxCursor{{Folder: "inbox", LastUID: 42,
		BackfillUID: &bf, UIDValidity: 7, RemoteName: "INBOX"}}); err != nil {
		t.Fatal(err)
	}
	emails := []Email{{
		ID: "me@qq.com:inbox:42", AccountID: "me@qq.com", Folder: "inbox",
		From: Contact{Name: "Alice", Email: "alice@x.com"}, To: []Contact{{Name: "我", Email: "me@qq.com"}},
		Subject: "备份测试", Body: "第一封。", Date: "2026-11-01T10:00:00+08:00", Read: true,
		Attachments: []Attachment{},
	}, {
		ID: "me@qq.com:sent:9", AccountID: "me@qq.com", Folder: "sent",
		From: Contact{Name: "我", Email: "me@qq.com"}, To: []Contact{{Name: "Alice", Email: "alice@x.com"}},
		Subject: "回复：备份测试", Body: "已回复。", Date: "2026-11-02T11:00:00+08:00",
		Real: true, UID: 9, RemoteName: "INBOX", Attachments: []Attachment{},
	}}
	if err := be.DB.UpsertEmails(emails); err != nil {
		t.Fatal(err)
	}
}

func TestBackupRoundTrip(t *testing.T) {
	src := backupTestEnv(t, "a")
	seedMailbox(t, src, "授权码-abc123")

	data, err := src.ExportBackup("口令✈pass", []byte(`{"theme":"dark","density":"compact"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data[:7]), backupMagic) {
		t.Fatal("备份文件缺少 magic 头")
	}

	// 目标机器：全新数据库、不同的密钥材料
	dst := backupTestEnv(t, "b")
	seedMailbox(t, dst, "旧设备自己的授权码") // 会被整体覆盖

	res, err := dst.ImportBackup("口令✈pass", data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Accounts != 1 || res.Emails != 2 {
		t.Fatalf("还原数量不符：%+v", res)
	}
	if !res.SecretChanged {
		t.Fatal("不同设备的密钥材料必然不同，SecretChanged 应为 true")
	}

	// 授权码能用还原后的密钥解出来
	cfg, err := dst.DB.LoadConfig("me@qq.com", dst.Crypto())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "授权码-abc123" {
		t.Fatalf("授权码还原错误：%q", cfg.Password)
	}

	// 同步游标原样迁移
	cs, err := dst.DB.LoadCursors("me@qq.com")
	if err != nil || len(cs) != 1 || cs[0].LastUID != 42 || cs[0].UIDValidity != 7 ||
		cs[0].BackfillUID == nil || *cs[0].BackfillUID != 500 {
		t.Fatalf("游标未还原：%v %v", cs, err)
	}

	// 邮件整体替换
	got, err := dst.DB.GetEmails()
	if err != nil || len(got) != 2 {
		t.Fatalf("邮件未还原：%d %v", len(got), err)
	}

	// 偏好设置带回界面
	if string(res.Settings) != `{"theme":"dark","density":"compact"}` {
		t.Fatalf("settings 未还原：%s", res.Settings)
	}

	// 密钥材料已热切换：同一 backend 立即用新密钥工作
	if dst.Secret() != src.Secret() {
		t.Fatal("还原后密钥材料未切换")
	}
}

func TestBackupThirdMachine(t *testing.T) {
	// 设备 A 导出 → 设备 B、C 先后还原：C 拿到的是 B 的密钥材料，同样能解授权码
	a := backupTestEnv(t, "a")
	seedMailbox(t, a, "pw-from-a")
	data, err := a.ExportBackup("同口令", nil)
	if err != nil {
		t.Fatal(err)
	}
	b := backupTestEnv(t, "b")
	if _, err := b.ImportBackup("同口令", data); err != nil {
		t.Fatal(err)
	}
	c := backupTestEnv(t, "c")
	seedMailbox(t, c, "pw-from-c")
	if _, err := c.ImportBackup("同口令", data); err != nil {
		t.Fatal(err)
	}
	cfg, err := c.DB.LoadConfig("me@qq.com", c.Crypto())
	if err != nil || cfg.Password != "pw-from-a" {
		t.Fatalf("第三台设备应能解开 A 的授权码：%q %v", cfg.Password, err)
	}
}

func TestBackupWrongPassphrase(t *testing.T) {
	be := backupTestEnv(t, "a")
	seedMailbox(t, be, "pw")
	data, err := be.ExportBackup("正确口令", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := be.ImportBackup("错误口令", data); err == nil ||
		!strings.Contains(err.Error(), "密码错误") {
		t.Fatalf("错误口令应明确报错：%v", err)
	}
	// 失败的还原不能动本地数据
	got, _ := be.DB.GetEmails()
	if len(got) != 2 {
		t.Fatalf("还原失败不应清空本地邮件：%d", len(got))
	}
}

func TestBackupTampered(t *testing.T) {
	be := backupTestEnv(t, "a")
	seedMailbox(t, be, "pw")
	data, err := be.ExportBackup("口令", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 破坏 magic
	bad := append([]byte(nil), data...)
	bad[0] = 'X'
	if _, err := be.ImportBackup("口令", bad); err == nil || !strings.Contains(err.Error(), "不是有效") {
		t.Fatalf("损坏文件应报错：%v", err)
	}
	// 篡改密文尾部
	bad = append([]byte(nil), data...)
	bad[len(bad)-1] ^= 0xff
	if _, err := be.ImportBackup("口令", bad); err == nil {
		t.Fatal("篡改密文应校验失败")
	}
	// 截断
	if _, err := be.ImportBackup("口令", data[:20]); err == nil {
		t.Fatal("截断文件应报错")
	}
}

func TestBackupWritesKeyFile(t *testing.T) {
	be := backupTestEnv(t, "a")
	seedMailbox(t, be, "pw")
	data, err := be.ExportBackup("口令", nil)
	if err != nil {
		t.Fatal(err)
	}
	dst := backupTestEnv(t, "b")
	res, err := dst.ImportBackup("口令", data)
	if err != nil {
		t.Fatal(err)
	}
	if !res.SecretChanged || res.Secret == "" {
		t.Fatalf("应带回密钥材料：%+v", res)
	}
	// app 层会用 WriteSecretFile 把材料写回 mail.key；这里直接验证工具函数
	dir := filepath.Join(t.TempDir(), "cfg")
	if err := WriteSecretFile(dir, res.Secret); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "mail.key"))
	if err != nil || string(b) != res.Secret {
		t.Fatalf("mail.key 写回失败：%q %v", b, err)
	}
}

func TestBackupShortPassphrase(t *testing.T) {
	be := backupTestEnv(t, "a")
	if _, err := be.ExportBackup("abc", nil); err == nil {
		t.Fatal("过短口令应拒绝")
	}
}
