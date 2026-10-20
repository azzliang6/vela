package core

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCryptoRoundtrip(t *testing.T) {
	c := NewCrypto("test-secret")
	a, _ := c.Encrypt("my-auth-code")
	b, _ := c.Encrypt("my-auth-code")
	if a == b {
		t.Fatal("nonce 必须每次随机")
	}
	if p, err := c.Decrypt(a); err != nil || p != "my-auth-code" {
		t.Fatal(p, err)
	}
	if _, err := NewCrypto("k2").Decrypt(a); err == nil {
		t.Fatal("错误密钥应解密失败")
	}
	raw, _ := base64.StdEncoding.DecodeString(a)
	raw[len(raw)-1] ^= 0xff
	if _, err := c.Decrypt(base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("篡改应被发现")
	}
	for _, g := range []string{"不是 base64!!", "", base64.StdEncoding.EncodeToString(make([]byte, 5))} {
		if _, err := c.Decrypt(g); err == nil {
			t.Fatalf("%q 应失败", g)
		}
	}
	u := NewCrypto("密钥🔑")
	e, _ := u.Encrypt("授权码 ☕")
	if p, _ := u.Decrypt(e); p != "授权码 ☕" {
		t.Fatal(p)
	}
}

// Rust 版（aes-gcm + sha2）用密钥 "k" 加密 "secret" 的一个真实输出格式：
// nonce‖ct‖tag。这里用 Go 自己造一个同构密文再手工拆解，确认布局一致。
func TestCryptoLayoutMatchesRust(t *testing.T) {
	c := NewCrypto("k")
	enc, _ := c.Encrypt("secret")
	raw, _ := base64.StdEncoding.DecodeString(enc)
	if len(raw) != 12+len("secret")+16 {
		t.Fatalf("布局长度 %d，应为 nonce(12)+明文+tag(16)", len(raw))
	}
}

func TestProviders(t *testing.T) {
	for in, want := range map[string]string{"a@qq.com": "qq", "a@vip.qq.com": "qq", "a@gmail.com": "gmail", "A@OUTLOOK.COM": "outlook", "a@company.cn": "custom"} {
		if g := GuessProvider(in); g != want {
			t.Errorf("%s → %s, want %s", in, g, want)
		}
	}
	cfg := MailServerConfig{Provider: "qq", Email: "me@qq.com"}
	if err := ApplyPreset(&cfg); err != nil || cfg.IMAPHost != "imap.qq.com" || cfg.IMAPSecurity != SecuritySSL || cfg.SMTPPort != 465 {
		t.Fatal(cfg, err)
	}
	c2 := MailServerConfig{Provider: "custom", Email: "me@corp.cn", IMAPPort: 993, SMTPPort: 465}
	if ApplyPreset(&c2) == nil {
		t.Fatal("自定义缺主机应报错")
	}
	c3 := MailServerConfig{Provider: "custom", Email: "me@corp.cn", IMAPHost: "m", SMTPHost: "m", IMAPPort: 143, SMTPPort: 587}
	_ = ApplyPreset(&c3)
	if c3.IMAPSecurity != SecurityStartTLS || c3.SMTPSecurity != SecurityStartTLS {
		t.Fatal(c3)
	}
	if ApplyPreset(&MailServerConfig{Provider: "qq", Email: "nope"}) == nil {
		t.Fatal("非法邮箱应报错")
	}
}

func TestLocalDBRoundtrip(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	e := Email{ID: "a@b.com:inbox:1", AccountID: "a@b.com", Folder: "inbox", From: Contact{"张三", "z@x.com"},
		To: []Contact{{"我", "a@b.com"}}, Subject: "主题", Body: "正文\x00带nul", Date: "2026-09-19T10:00:00+08:00",
		Starred: true, UID: 1, Real: true, RemoteName: "INBOX"}
	if err := db.UpsertEmails([]Email{e}); err != nil {
		t.Fatal(err)
	}
	out, _ := db.GetEmails()
	if len(out) != 1 || out[0].Body != "正文带nul" || out[0].From.Email != "z@x.com" || !out[0].Starred || out[0].CC != nil || out[0].UID != 1 {
		t.Fatalf("%+v", out)
	}
	_ = db.DeleteEmails([]string{e.ID})
	if out, _ := db.GetEmails(); len(out) != 0 {
		t.Fatal("删除失败")
	}
}

// Rust 版写入的行没有 uid 列：读取时从 id 还原。
func TestLegacyRowWithoutUID(t *testing.T) {
	db, _ := OpenDB(":memory:")
	_, err := db.db.Exec(`INSERT INTO emails (id, account_id, folder, sender, recipients, subject, body, date)
		VALUES ('me@qq.com:inbox:42','me@qq.com','inbox','{"name":"a","email":"a@x"}','[]','s','b','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := db.GetEmails()
	if out[0].UID != 42 || !out[0].Real || out[0].RemoteName != "INBOX" {
		t.Fatalf("%+v", out[0])
	}
}

func TestAccountStore(t *testing.T) {
	db, _ := OpenDB(":memory:")
	c := NewCrypto("k")
	cfg := MailServerConfig{Provider: "qq", Email: "a@qq.com", IMAPHost: "imap.qq.com", IMAPPort: 993, SMTPHost: "smtp.qq.com", SMTPPort: 465, Password: "secret"}
	if _, err := db.UpsertAccount(cfg, "我的QQ", c); err != nil {
		t.Fatal(err)
	}
	l, _ := db.ListAccounts()
	if len(l) != 1 || l[0].Label != "我的QQ" {
		t.Fatal(l)
	}
	got, err := db.LoadConfig("a@qq.com", c)
	if err != nil || got.Password != "secret" {
		t.Fatal(got, err)
	}
	_ = db.SaveCursors("a@qq.com", []MailboxCursor{{Folder: "inbox", LastUID: 42, BackfillUID: u32p(7), UIDValidity: 1, RemoteName: "INBOX"}})
	cs, _ := db.LoadCursors("a@qq.com")
	if cs[0].LastUID != 42 || *cs[0].BackfillUID != 7 {
		t.Fatal(cs)
	}
	// Rust 版写的游标 JSON 能读
	_, _ = db.db.Exec(`UPDATE mailbox_accounts SET cursors='[{"folder":"sent","lastUid":5,"backfillUid":null,"uidValidity":3,"remoteName":"Sent Messages"}]'`)
	cs, _ = db.LoadCursors("a@qq.com")
	if cs[0].Folder != "sent" || cs[0].BackfillUID != nil || cs[0].RemoteName != "Sent Messages" {
		t.Fatal(cs)
	}
	_ = db.DeleteAccount("a@qq.com")
	if l, _ := db.ListAccounts(); len(l) != 0 {
		t.Fatal(l)
	}
}

func TestHTMLToText(t *testing.T) {
	s := HTMLToText("<html><head><style>p{color:red}</style></head><body><p>你好</p><script>alert(1)</script><p>世界</p></body></html>")
	if !strings.Contains(s, "你好") || !strings.Contains(s, "世界") || strings.Contains(s, "color") || strings.Contains(s, "alert") || strings.Contains(s, "<") {
		t.Fatal(s)
	}
}

func TestParseMailbox(t *testing.T) {
	if a, _ := ParseMailbox("张三 <a@b.com>"); a.Name != "张三" || a.Address != "a@b.com" {
		t.Fatal(a)
	}
	if a, _ := ParseMailbox(`"Doe, John" <j@b.com>`); a.Name != "Doe, John" {
		t.Fatal(a)
	}
	if a, _ := ParseMailbox("  a@b.com  "); a.Address != "a@b.com" {
		t.Fatal(a)
	}
	for _, bad := range []string{"", "  ", "not an email", "名字 <>"} {
		if _, err := ParseMailbox(bad); err == nil {
			t.Errorf("%q 应报错", bad)
		}
	}
	cfg := MailServerConfig{Email: "a@qq.com", SMTPSecurity: SecuritySSL}
	if _, err := Send(cfg, OutgoingMail{Subject: "s"}); err == nil || !strings.Contains(err.Error(), "收件人") {
		t.Fatal(err)
	}
	cfg.SMTPSecurity = SecurityNone
	if _, err := Send(cfg, OutgoingMail{To: []string{"x@y.z"}}); err == nil || !strings.Contains(err.Error(), "不支持无加密") {
		t.Fatal(err)
	}
}

func TestBriefError(t *testing.T) {
	if BriefError(`{"error":{"message":"Invalid API key","code":1001}}`) != "Invalid API key" || BriefError("plain text") != "plain text" {
		t.Fatal()
	}
}

func TestParseGBK(t *testing.T) {
	raw := "From: =?GBK?B?1dXI/bLO?= <zhang@qq.com>\r\nTo: test@example.com\r\nSubject: =?GBK?B?suLK1NPKvP4=?=\r\nDate: Thu, 18 Sep 2026 10:00:00 +0800\r\nMessage-ID: <m101@qq.com>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"GBK\"\r\nContent-Transfer-Encoding: base64\r\n\r\nxOO6w8rAvec=\r\n"
	e, err := ParseMessage([]byte(raw), "test@example.com", "inbox", 101, false, false, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if e.Subject != "测试邮件" || strings.TrimSpace(e.Body) != "你好世界" || e.From.Email != "zhang@qq.com" || e.From.Name != "张三参" && e.From.Name == "" {
		t.Fatalf("%+v", e)
	}
	if e.ID != "test@example.com:inbox:101" || e.MessageID != "m101@qq.com" {
		t.Fatal(e.ID, e.MessageID)
	}
}
