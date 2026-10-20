package core

// 端到端测试：对 scripts/mock_mail_server.py 跑真实的 IMAP/SMTP 协议，
// 对应原 src-tauri/tests/imap_e2e.rs 与 backfill_e2e.rs。
// 没有 python3 时跳过。

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startMock 启动一个独立的模拟服务器，返回 IMAP/SMTP 端口。
func startMock(t *testing.T, bulk bool) (int, int) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("跳过：没有 python3，无法启动模拟邮件服务器")
	}
	_, file, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(file), "..", "..", "scripts", "mock_mail_server.py")
	ip, sp := freePort(t), freePort(t)
	cmd := exec.Command(py, script, fmt.Sprint(ip), fmt.Sprint(sp))
	cmd.Env = os.Environ()
	if bulk {
		cmd.Env = append(cmd.Env, "MOCK_BULK=1")
	}
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	sc := bufio.NewScanner(out)
	deadline := time.Now().Add(10 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		if strings.HasPrefix(sc.Text(), "SMTP ready") {
			break
		}
	}
	allowInsecure = true
	t.Cleanup(func() { allowInsecure = false })
	return ip, sp
}

func mockCfg(ip, sp int) MailServerConfig {
	return MailServerConfig{Provider: "custom", Email: "test@example.com", Password: "secret123",
		IMAPHost: "127.0.0.1", IMAPPort: ip, IMAPSecurity: SecurityNone,
		SMTPHost: "127.0.0.1", SMTPPort: sp, SMTPSecurity: SecurityNone}
}

func TestE2ESyncFetchAndParse(t *testing.T) {
	ip, sp := startMock(t, false)
	res, err := SyncAccount(mockCfg(ip, sp), nil)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Email{}
	for _, m := range res.Mails {
		byID[m.ID] = m
	}
	m := byID["test@example.com:inbox:101"]
	if m.Subject != "测试邮件" || strings.TrimSpace(m.Body) != "你好世界" || m.From.Email != "zhang@qq.com" {
		t.Fatalf("GBK 解码失败：%+v", m)
	}
	if !byID["test@example.com:inbox:102"].Read {
		t.Error("102 应为已读")
	}
	if s := byID["test@example.com:sent:201"]; s.RemoteName != "Sent Messages" || s.Folder != "sent" {
		t.Errorf("已发送：%+v", s)
	}
	if len(res.Present) != 2 {
		t.Errorf("present %+v", res.Present)
	}
	for _, m := range res.Mails {
		if m.IsNewArrival {
			t.Error("首次同步不应标记新到")
		}
	}
	// 游标可续：再同步一次没有新邮件
	res2, err := SyncAccount(mockCfg(ip, sp), res.Cursors)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Mails) != 0 {
		t.Errorf("续同步不应重复拉取：%d 封", len(res2.Mails))
	}
}

func TestE2ELoginRejectsBadPassword(t *testing.T) {
	ip, sp := startMock(t, false)
	cfg := mockCfg(ip, sp)
	cfg.Password = "wrong"
	if _, err := TestConnection(cfg); err == nil || !strings.Contains(err.Error(), "认证失败") {
		t.Fatal(err)
	}
	names, err := TestConnection(mockCfg(ip, sp))
	if err != nil || len(names) < 4 {
		t.Fatal(names, err)
	}
}

func TestE2EFlagsAndMove(t *testing.T) {
	ip, sp := startMock(t, false)
	cfg := mockCfg(ip, sp)
	on, off := true, false
	if err := SetFlags(cfg, "Sent Messages", 201, nil, &on); err != nil {
		t.Fatal(err)
	}
	res, _ := SyncAccount(cfg, nil)
	for _, m := range res.Mails {
		if m.UID == 201 && !(m.Starred && m.Read) {
			t.Fatalf("星标未生效或丢了已读：%+v", m)
		}
	}
	_ = SetFlags(cfg, "Sent Messages", 201, nil, &off)
	// 移动：探测归档文件夹会切换选中邮箱，必须重新选中源文件夹后再 MOVE
	if err := MoveMessage(cfg, "INBOX", 102, FolderArchive); err != nil {
		t.Fatal(err)
	}
	res, _ = SyncAccount(cfg, nil)
	for _, p := range res.Present {
		if p.Folder == "inbox" {
			for _, u := range p.UIDs {
				if u == 102 {
					t.Fatal("102 应已移出收件箱")
				}
			}
		}
		if p.Folder == "sent" && len(p.UIDs) != 1 {
			t.Fatalf("已发送不应被误动：%v", p.UIDs)
		}
	}
	if err := MoveMessage(cfg, "INBOX", 101, FolderTrash); err != nil {
		t.Fatal(err)
	}
}

func TestE2ESMTPDelivers(t *testing.T) {
	ip, sp := startMock(t, false)
	r, err := Send(mockCfg(ip, sp), OutgoingMail{To: []string{"张三 <bob@example.org>"}, CC: []string{"c@example.org"},
		InReplyTo: "m101@qq.com", Subject: "你好", Body: "第一行\n第二行"})
	if err != nil || r == "" {
		t.Fatal(r, err)
	}
}

func TestE2EBackfillCoversEverything(t *testing.T) {
	ip, sp := startMock(t, true)
	cfg := mockCfg(ip, sp)
	var cursors []MailboxCursor
	seen := map[uint32]int{}
	rounds := 0
	for {
		rounds++
		if rounds > 20 {
			t.Fatal("回填未收敛")
		}
		res, err := SyncAccount(cfg, cursors)
		if err != nil {
			t.Fatal(err)
		}
		if rounds == 1 && (len(res.Mails) != MaxFetchPerSync || res.Mails[0].UID != 701) {
			t.Fatalf("首批应是最新的 300 封，得到 %d 封，首封 %d", len(res.Mails), res.Mails[0].UID)
		}
		for _, m := range res.Mails {
			if m.Folder == "inbox" {
				seen[m.UID]++
			}
		}
		cursors = res.Cursors
		c := findCursor(cursors, "inbox")
		if c.BackfillUID != nil && *c.BackfillUID == 0 {
			if c.LastUID != 1000 {
				t.Fatalf("lastUid %d", c.LastUID)
			}
			break
		}
	}
	if rounds < 4 {
		t.Errorf("1000 封应至少 4 轮，实际 %d", rounds)
	}
	if len(seen) != 1000 {
		t.Fatalf("覆盖 %d 封，应为 1000", len(seen))
	}
	for u, n := range seen {
		if n != 1 {
			t.Fatalf("%d 下载了 %d 次", u, n)
		}
	}
}
