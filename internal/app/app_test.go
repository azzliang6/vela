package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo/ui"
)

// fakeBackend 是内存里的后端，记录界面发起的调用。
type fakeBackend struct {
	accounts []core.MailboxAccount
	emails   []core.Email
	sync     map[string]core.SyncOutcome
	sent     []core.SendInput
	flags    []string
	moved    []string
	added    []core.AddAccountInput
	addErr   error
	saved    int
}

func (f *fakeBackend) Providers() []core.ProviderPreset { return core.AllPresets() }
func (f *fakeBackend) GuessProvider(e string) string    { return core.GuessProvider(e) }
func (f *fakeBackend) TestConnection(core.AddAccountInput) ([]string, error) {
	return []string{"INBOX"}, nil
}
func (f *fakeBackend) AddAccount(in core.AddAccountInput) (core.MailboxAccount, error) {
	f.added = append(f.added, in)
	if f.addErr != nil {
		return core.MailboxAccount{}, f.addErr
	}
	a := core.MailboxAccount{ID: in.Email, Email: in.Email, Label: in.Email, Provider: in.Provider}
	f.accounts = append(f.accounts, a)
	return a, nil
}
func (f *fakeBackend) ListAccounts() ([]core.MailboxAccount, error) { return f.accounts, nil }
func (f *fakeBackend) RemoveAccount(string) error                  { return nil }
func (f *fakeBackend) SyncAccount(id string) (core.SyncOutcome, error) {
	if o, ok := f.sync[id]; ok {
		return o, nil
	}
	return core.SyncOutcome{AccountID: id}, nil
}
func (f *fakeBackend) SetFlags(acc, remote string, uid uint32, read, starred *bool) error {
	f.flags = append(f.flags, remote)
	return nil
}
func (f *fakeBackend) Archive(acc, remote string, uid uint32) error {
	f.moved = append(f.moved, "archive")
	return nil
}
func (f *fakeBackend) Trash(acc, remote string, uid uint32) error {
	f.moved = append(f.moved, "trash")
	return nil
}
func (f *fakeBackend) Send(in core.SendInput) (string, error) {
	f.sent = append(f.sent, in)
	return "已投递", nil
}
func (f *fakeBackend) GetEmails() ([]core.Email, error)    { return f.emails, nil }
func (f *fakeBackend) SyncEmails(e []core.Email) error     { f.saved += len(e); return nil }
func (f *fakeBackend) DeleteEmails([]string) error         { return nil }
func (f *fakeBackend) AIChat(core.AIChatInput) (string, error) {
	return "```json\n{\"category\":\"finance\",\"summary\":\"信用卡账单 ¥1,234 待还\",\"why\":\"\"}\n```", nil
}

func mail(id, subject, from string, read bool) core.Email {
	return core.Email{ID: "me@qq.com:inbox:" + id, AccountID: "me@qq.com", Folder: "inbox", Subject: subject,
		From: core.Contact{Name: from, Email: strings.ToLower(from) + "@x.com"}, To: []core.Contact{{Name: "我", Email: "me@qq.com"}},
		Body: subject + " 的正文。\n\n第二段。", Date: "2026-10-1" + id[len(id)-1:] + "T10:00:00+08:00", Read: read,
		Real: true, UID: 1, RemoteName: "INBOX", MessageID: "m" + id + "@x.com", Attachments: []core.Attachment{}}
}

func newTestApp() (*App, *fakeBackend) {
	f := &fakeBackend{
		accounts: []core.MailboxAccount{{ID: "me@qq.com", Email: "me@qq.com", Label: "我的 QQ"}},
		emails:   []core.Email{mail("1", "周会纪要", "Alice", true), mail("2", "信用卡账单", "Bank", false)},
	}
	return New(f, ""), f
}

func TestListAndReadMail(t *testing.T) {
	a, f := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	for _, s := range []string{"收件箱", "周会纪要", "信用卡账单", "我的 QQ", "选择一封邮件开始阅读"} {
		if !tt.HasText(s) {
			t.Fatalf("缺少 %q：%q", s, tt.Texts())
		}
	}
	if err := tt.Click("信用卡账单"); err != nil {
		t.Fatal(err)
	}
	if a.Selected() == nil || a.Selected().Subject != "信用卡账单" || !a.Selected().Read {
		t.Fatal("应打开并标为已读")
	}
	if len(f.flags) != 1 || f.flags[0] != "INBOX" {
		t.Fatalf("已读应回写到服务器：%v", f.flags)
	}
	if !tt.HasText("第二段。") {
		t.Errorf("纯文本正文未显示：%q", tt.Texts())
	}
}

func TestFolderSwitchAndStar(t *testing.T) {
	a, _ := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("周会纪要")
	if err := tt.Click("加星标"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("星标"); err != nil {
		t.Fatal(err)
	}
	if a.folder != "starred" || len(a.Visible()) != 1 || !tt.HasText("周会纪要") {
		t.Fatalf("星标文件夹：%q", tt.Texts())
	}
	_ = tt.Click("已发送")
	if !tt.HasText("这里空空如也") {
		t.Errorf("空文件夹提示：%q", tt.Texts())
	}
}

func TestArchiveAndTrash(t *testing.T) {
	a, f := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("周会纪要")
	if err := tt.Click("归档此邮件"); err != nil {
		t.Fatal(err)
	}
	if len(a.Visible()) != 1 || len(f.moved) != 1 || f.moved[0] != "archive" {
		t.Fatalf("归档：%v", f.moved)
	}
	_ = tt.Click("信用卡账单")
	_ = tt.Click("删除此邮件")
	_ = tt.Click("垃圾箱")
	if !tt.HasText("信用卡账单") {
		t.Fatal("垃圾箱里应有被删的邮件")
	}
}

func TestSearch(t *testing.T) {
	a, _ := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("搜索")
	tt.Type("账单")
	tt.Frame()
	if len(a.Visible()) != 1 || tt.HasText("周会纪要") {
		t.Fatalf("搜索过滤：%q", tt.Texts())
	}
}

func TestComposeReplyAndSend(t *testing.T) {
	a, f := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("周会纪要")
	if err := tt.Click("回复"); err != nil {
		t.Fatal(err)
	}
	if !a.composeOpen || a.compose.To != "alice@x.com" || a.compose.Subject != "Re: 周会纪要" || a.compose.InReplyTo != "m1@x.com" {
		t.Fatalf("回复草稿：%+v", a.compose)
	}
	if err := tt.Click("发送"); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || f.sent[0].To[0] != "alice@x.com" || f.sent[0].AccountID != "me@qq.com" {
		t.Fatalf("发送：%+v", f.sent)
	}
	if a.composeOpen {
		t.Error("发送后应关闭写信窗口")
	}
	a.SetFolder("sent")
	if len(a.Visible()) != 1 {
		t.Error("已发送里应有本地副本")
	}
}

func TestComposeNewTyped(t *testing.T) {
	a, f := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Key(ui.Cmd, ui.KeyN)
	if !a.composeOpen {
		t.Fatal("⌘N 应打开写信")
	}
	tt.Type("bob@example.org")
	_ = tt.Click("发送")
	if len(f.sent) != 1 || f.sent[0].To[0] != "bob@example.org" {
		t.Fatalf("%+v", f.sent)
	}
}

func TestAddMailbox(t *testing.T) {
	a, f := newTestApp()
	f.addErr = errors.New("认证失败：请确认使用的是「授权码」")
	tt := ui.NewTester(a.View, 1280, 820)
	if err := tt.Click("添加邮箱"); err != nil {
		t.Fatal(err)
	}
	tt.Type("new@163.com")
	tt.Frame()
	if a.add.Provider != "网易 163" {
		t.Errorf("应按域名自动选服务商，得到 %q", a.add.Provider)
	}
	a.add.Password = "code"
	_ = tt.Click("连接并添加")
	if !tt.HasText("认证失败：请确认使用的是「授权码」") || !a.addOpen {
		t.Fatalf("错误应显示在表单里：%q", tt.Texts())
	}
	f.addErr = nil
	_ = tt.Click("连接并添加")
	if a.addOpen || len(a.accounts) != 2 || f.added[1].Provider != "163" {
		t.Fatalf("添加后：%+v %+v", a.accounts, f.added)
	}
}

func TestSyncMergesPrunesAndNotifies(t *testing.T) {
	a, f := newTestApp()
	var notes []string
	a.notify = func(title, body string) { notes = append(notes, title+"|"+body) }
	n := mail("3", "明天务必提交", "Boss", false)
	n.IsNewArrival = true
	f.sync = map[string]core.SyncOutcome{"me@qq.com": {AccountID: "me@qq.com", Emails: []core.Email{n},
		Present: []core.FolderUIDs{{Folder: "inbox", UIDs: []uint32{1}}}}}
	// 让 1 号保留、2 号在服务器上已不存在
	a.emails[1].UID = 2
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("立即同步")
	if !tt.HasText("明天务必提交") || tt.HasText("信用卡账单") {
		t.Fatalf("合并 / 剔除：%q", tt.Texts())
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "Boss：明天务必提交") {
		t.Fatalf("通知：%v", notes)
	}
	if _, e := a.find(n.ID); e.Category != "important" {
		t.Errorf("新邮件应按规则分类为重要，得到 %q", e.Category)
	}
}

func TestAIAnalyze(t *testing.T) {
	a, _ := newTestApp()
	a.settings.AIKey = "k"
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("信用卡账单")
	if err := tt.Click("AI 分析"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("信用卡账单 ¥1,234 待还") || !tt.HasText("AI 摘要 · 财务") {
		t.Fatalf("AI 摘要：%q", tt.Texts())
	}
}

func TestSettingsDarkTheme(t *testing.T) {
	a, _ := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("设置")
	if err := tt.Click("深色"); err != nil {
		t.Fatal(err)
	}
	_ = tt.Click("完成")
	if a.settings.Theme != "dark" || !a.Dark() {
		t.Fatal("应切换到深色")
	}
}

func TestIdleAndNoLoops(t *testing.T) {
	a, _ := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("周会纪要")
	tt.Advance(2 * time.Second)
	if tt.FramePending() {
		t.Fatal("空闲时不应持续请求帧（CPU 应为 0）")
	}
}

func TestSanitize(t *testing.T) {
	out := SanitizeMailHTML(`<p onclick="x()">hi<script>alert(1)</script><img src="https://e.com/a.png"><iframe src="x"></iframe><a href="javascript:alert(1)">l</a></p>`, false)
	for _, bad := range []string{"<script", "onclick", "<iframe", "javascript:"} {
		if strings.Contains(out, bad) {
			t.Errorf("未清洗：%s\n%s", bad, out)
		}
	}
	if !strings.Contains(out, `img src="https://e.com/a.png"`) || !strings.Contains(out, "script-src 'none'") {
		t.Errorf("应保留图片并带 CSP：%s", out)
	}
}

func TestRules(t *testing.T) {
	e := &core.Email{Subject: "本月信用卡账单", Body: "您的账单金额 ¥300，请于 10 日前还款。"}
	if Classify(e) != "finance" {
		t.Fatal(Classify(e))
	}
	if r := ParseAnalysis("好的：{\"category\":\"travel\",\"summary\":\"航班改签\"}"); r.Category != "travel" || r.Summary != "航班改签" {
		t.Fatal(r)
	}
	if r := ParseAnalysis("模型乱说"); r.Summary != "模型乱说" || r.Category != "" {
		t.Fatal(r)
	}
}
