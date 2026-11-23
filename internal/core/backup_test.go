package core

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// backupTestEnv 在独立目录里打开一个全新的“设备”。secret 非空时模拟设置了
// VELA_SECRET 环境变量的机器（密钥固定在环境变量里，不读 mail.key）。
func backupTestEnv(t *testing.T, sub, secret string) *Backend {
	t.Helper()
	oldIters := backupKDFIters
	backupKDFIters = 4096 // 测试里调小 PBKDF2 迭代次数以保持速度
	t.Cleanup(func() { backupKDFIters = oldIters })
	// t.TempDir() 在同一个测试里每次返回同一路径，用子目录区分多台“设备”
	dir := filepath.Join(t.TempDir(), sub)
	t.Setenv("VELA_DATA_DIR", dir)
	t.Setenv("VELA_SECRET", secret)
	be, err := OpenBackend()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { be.DB.Close() })
	return be
}

func snapshotDirOf(be *Backend) string {
	return filepath.Join(filepath.Dir(be.DB.path), snapshotDirName)
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
	src := backupTestEnv(t, "a", "")
	seedMailbox(t, src, "授权码-abc123")

	data, err := src.ExportBackup("口令✈pass", []byte(`{"theme":"dark","density":"compact"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:len(backupMagic)]) != backupMagic {
		t.Fatal("备份文件缺少 magic 头")
	}

	// 目标机器：另一把密钥，上面已有会被覆盖的旧数据
	dst := backupTestEnv(t, "b", "")
	seedMailbox(t, dst, "旧设备自己的授权码")
	localSecret, keyFile := dst.Secret(), filepath.Join(DataDir(), "mail.key")
	keyBefore, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}

	res, err := dst.ImportBackup("口令✈pass", data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Accounts != 1 || res.Emails != 2 {
		t.Fatalf("还原数量不符：%+v", res)
	}

	// 授权码用备份里的密钥解开，再加密到了本机密钥下；本机密钥和 mail.key 原样不动
	cfg, err := dst.DB.LoadConfig("me@qq.com", dst.Crypto())
	if err != nil || cfg.Password != "授权码-abc123" {
		t.Fatalf("授权码还原错误：%q %v", cfg.Password, err)
	}
	if dst.Secret() != localSecret {
		t.Fatal("还原不应更换本机密钥")
	}
	if keyAfter, err := os.ReadFile(keyFile); err != nil || string(keyAfter) != string(keyBefore) {
		t.Fatalf("还原不应改写 mail.key：%q %v", keyAfter, err)
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
}

func TestBackupThirdMachine(t *testing.T) {
	// 设备 A 导出 → 设备 B、C 先后还原：两台都用自己的密钥保管 A 的授权码
	a := backupTestEnv(t, "a", "")
	seedMailbox(t, a, "pw-from-a")
	data, err := a.ExportBackup("同口令", nil)
	if err != nil {
		t.Fatal(err)
	}
	b := backupTestEnv(t, "b", "")
	if _, err := b.ImportBackup("同口令", data); err != nil {
		t.Fatal(err)
	}
	c := backupTestEnv(t, "c", "")
	seedMailbox(t, c, "pw-from-c")
	if _, err := c.ImportBackup("同口令", data); err != nil {
		t.Fatal(err)
	}
	for name, be := range map[string]*Backend{"b": b, "c": c} {
		if cfg, err := be.DB.LoadConfig("me@qq.com", be.Crypto()); err != nil || cfg.Password != "pw-from-a" {
			t.Fatalf("设备 %s 应能解开 A 的授权码：%q %v", name, cfg.Password, err)
		}
	}
}

func TestBackupEnvSecretSurvivesRestart(t *testing.T) {
	src := backupTestEnv(t, "a", "")
	seedMailbox(t, src, "pw-from-a")
	data, err := src.ExportBackup("口令", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 目标机器用 VELA_SECRET 固定密钥：还原不能去动这把密钥
	dst := backupTestEnv(t, "b", "固定在环境变量里的密钥")
	if _, err := dst.ImportBackup("口令", data); err != nil {
		t.Fatal(err)
	}
	dst.DB.Close()

	// 重启：同一个数据目录重新打开，密钥仍然来自环境变量
	restarted, err := OpenBackend()
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.DB.Close()
	if restarted.Secret() != "固定在环境变量里的密钥" {
		t.Fatalf("重启后密钥应仍来自环境变量：%q", restarted.Secret())
	}
	if cfg, err := restarted.DB.LoadConfig("me@qq.com", restarted.Crypto()); err != nil || cfg.Password != "pw-from-a" {
		t.Fatalf("重启后授权码应仍能解开：%q %v", cfg.Password, err)
	}
}

func TestBackupRejectsUnreadableCredentials(t *testing.T) {
	src := backupTestEnv(t, "a", "")
	seedMailbox(t, src, "pw")
	// 伪造一份密钥材料与授权码密文对不上的备份（来源设备的 mail.key 丢失后导出）
	accounts, err := src.DB.ExportAccountsRaw()
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(backupEnvelope{App: "vela", Format: backupVersion, Secret: "另一把密钥", Accounts: accounts})
	data, err := sealBackup(payload, "口令")
	if err != nil {
		t.Fatal(err)
	}

	dst := backupTestEnv(t, "b", "")
	seedMailbox(t, dst, "本机授权码")
	if _, err := dst.ImportBackup("口令", data); err == nil || !strings.Contains(err.Error(), "授权码无法解密") {
		t.Fatalf("授权码解不开时应拒绝还原：%v", err)
	}
	// 什么都没动：本机的账户和邮件照旧，也没有留下副本
	if cfg, err := dst.DB.LoadConfig("me@qq.com", dst.Crypto()); err != nil || cfg.Password != "本机授权码" {
		t.Fatalf("本机授权码被改动了：%q %v", cfg.Password, err)
	}
	if got, _ := dst.DB.GetEmails(); len(got) != 2 {
		t.Fatalf("本机邮件被改动了：%d", len(got))
	}
	if _, err := os.Stat(snapshotDirOf(dst)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("取消的还原不应留下副本：%v", err)
	}
}

func TestBackupWrongPassphrase(t *testing.T) {
	be := backupTestEnv(t, "a", "")
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
	be := backupTestEnv(t, "a", "")
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

func TestBackupShortPassphrase(t *testing.T) {
	be := backupTestEnv(t, "a", "")
	if _, err := be.ExportBackup("abc", nil); err == nil {
		t.Fatal("过短口令应拒绝")
	}
}

/* ———— 还原前的数据副本 ———— */

func TestRestoreKeepsSnapshot(t *testing.T) {
	src := backupTestEnv(t, "a", "")
	seedMailbox(t, src, "pw-from-a")
	data, err := src.ExportBackup("口令", nil)
	if err != nil {
		t.Fatal(err)
	}

	dst := backupTestEnv(t, "b", "")
	seedMailbox(t, dst, "本机授权码")
	if err := dst.DB.UpsertEmails([]Email{{ID: "me@qq.com:inbox:1", AccountID: "me@qq.com", Folder: "inbox",
		Subject: "只在本机有的邮件", Body: "还原后应该从当前数据里消失", Date: "2026-11-03T10:00:00+08:00",
		Attachments: []Attachment{}}}); err != nil {
		t.Fatal(err)
	}

	res, err := dst.ImportBackup("口令", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(res.Snapshot), snapshotPrefix) || filepath.Dir(res.Snapshot) != snapshotDirOf(dst) {
		t.Fatalf("覆盖前应在 snapshots 目录留下副本：%+v", res)
	}
	entries, err := os.ReadDir(snapshotDirOf(dst))
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshots 目录应只有这一份副本：%v %v", entries, err)
	}

	// 当前数据已经被备份覆盖
	now, _ := dst.DB.GetEmails()
	if len(now) != 2 {
		t.Fatalf("本机原有的邮件应已被覆盖：%d", len(now))
	}

	// 副本是还原那一刻的完整数据：本机原有的邮件和授权码都在，而且仍用本机密钥加密，
	// 所以把副本换回 vela.db 就能回到还原之前
	snap, err := OpenDB(res.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	old, err := snap.GetEmails()
	found := false
	for _, e := range old {
		found = found || e.ID == "me@qq.com:inbox:1"
	}
	if err != nil || len(old) != 3 || !found {
		t.Fatalf("副本里应有还原前的 3 封邮件：%d %v", len(old), err)
	}
	if cfg, err := snap.LoadConfig("me@qq.com", dst.Crypto()); err != nil || cfg.Password != "本机授权码" {
		t.Fatalf("副本里的授权码应能用本机密钥解开：%q %v", cfg.Password, err)
	}
}

func TestRestoreOnEmptyMachineLeavesNoSnapshot(t *testing.T) {
	src := backupTestEnv(t, "a", "")
	seedMailbox(t, src, "pw")
	data, err := src.ExportBackup("口令", nil)
	if err != nil {
		t.Fatal(err)
	}
	dst := backupTestEnv(t, "b", "")
	res, err := dst.ImportBackup("口令", data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Snapshot != "" {
		t.Fatalf("本机原本没有数据，不需要留副本：%q", res.Snapshot)
	}
	if _, err := os.Stat(snapshotDirOf(dst)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("空机器还原后不应有 snapshots 目录：%v", err)
	}
}

func TestSnapshotsKeepNewest(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"vela-before-restore-20261101-000001.db", "vela-before-restore-20261102-000001.db",
		"vela-before-restore-20261103-000001-2.db", "vela-before-restore-20261104-000001.db",
		"vela-before-restore-20261105-000001.db", "vela.db", "settings.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 文件名的字典序不是时间序（带序号后缀的那份排在后面），应按修改时间取舍
	base := time.Date(2026, 11, 1, 0, 0, 1, 0, time.UTC)
	for i, name := range []string{"vela-before-restore-20261101-000001.db", "vela-before-restore-20261102-000001.db",
		"vela-before-restore-20261103-000001-2.db", "vela-before-restore-20261104-000001.db",
		"vela-before-restore-20261105-000001.db"} {
		if err := os.Chtimes(filepath.Join(dir, name), base.AddDate(0, 0, i), base.AddDate(0, 0, i)); err != nil {
			t.Fatal(err)
		}
	}
	pruneSnapshots(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := "settings.json, vela-before-restore-20261103-000001-2.db, vela-before-restore-20261104-000001.db, " +
		"vela-before-restore-20261105-000001.db, vela.db"
	if strings.Join(got, ", ") != want {
		t.Fatalf("应删掉最旧的两份副本、保留其余文件：\n got  %v\n want %s", got, want)
	}
}

/* ———— 还原之后后台监控跟上新的账户列表 ———— */

// closedPort 返回一个本机上确定没有人在监听的端口：监控线程连上去会立刻被拒绝，
// 退避等待，测试借此观察监控线程的启停而不会访问外网。
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func seedLocal(t *testing.T, be *Backend, email string, port int) {
	t.Helper()
	if _, err := be.DB.UpsertAccount(MailServerConfig{Provider: "custom", Email: email,
		IMAPHost: "127.0.0.1", IMAPPort: port, IMAPSecurity: SecuritySSL,
		SMTPHost: "127.0.0.1", SMTPPort: port, SMTPSecurity: SecuritySSL, Password: "pw-" + email},
		email, be.Crypto()); err != nil {
		t.Fatal(err)
	}
}

// waitRunning 等待监控线程的运行表恰好等于 want（true = 应在监控，false = 不应在监控）。
func waitRunning(t *testing.T, w *Watcher, want map[string]bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		w.mu.Lock()
		got := map[string]bool{}
		for id := range w.running {
			got[id] = true
		}
		w.mu.Unlock()
		ok := len(got) == len(want)
		for id, running := range want {
			ok = ok && got[id] == running
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("监控的账户不符合预期：got %v，want %v", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWatcherRescan(t *testing.T) {
	port := closedPort(t)
	be := backupTestEnv(t, "a", "")
	seedLocal(t, be, "one@x", port)
	w := be.StartWatcher(func(string, []Email) {})
	defer w.Stop()
	waitRunning(t, w, map[string]bool{"one@x": true})

	// 新加的账户不必等 45 秒的扫描周期
	seedLocal(t, be, "two@x", port)
	w.Rescan()
	waitRunning(t, w, map[string]bool{"one@x": true, "two@x": true})

	// 删掉的账户，连接被收掉
	if err := be.DB.DeleteAccount("one@x"); err != nil {
		t.Fatal(err)
	}
	w.Rescan()
	waitRunning(t, w, map[string]bool{"two@x": true})
}

func TestRestoreReconnectsWatcher(t *testing.T) {
	port := closedPort(t)
	dst := backupTestEnv(t, "b", "")
	seedLocal(t, dst, "old@x", port)
	w := dst.StartWatcher(func(string, []Email) {})
	defer w.Stop()
	waitRunning(t, w, map[string]bool{"old@x": true})

	src := backupTestEnv(t, "a", "")
	seedLocal(t, src, "new@x", port)
	data, err := src.ExportBackup("口令", nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := dst.ImportBackup("口令", data); err != nil {
		t.Fatal(err)
	}
	// 还原之后监控立刻换成备份里的账户：新的开始监控，被覆盖的旧账户断开
	waitRunning(t, w, map[string]bool{"new@x": true})
}
