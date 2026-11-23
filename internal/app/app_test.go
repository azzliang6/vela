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
	accounts  []core.MailboxAccount
	emails    []core.Email
	sync      map[string]core.SyncOutcome
	sent      []core.SendInput
	flags     []string
	moved     []string
	added     []core.AddAccountInput
	addErr    error
	saved     int
	htmlReads int
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
func (f *fakeBackend) RemoveAccount(string) error                   { return nil }
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

// GetEmails 与真实后端一样不带 HTML 正文，只标记 HasHTML。
func (f *fakeBackend) GetEmails() ([]core.Email, error) {
	out := make([]core.Email, len(f.emails))
	for i, e := range f.emails {
		e.HasHTML, e.HTMLBody = e.HTMLBody != "", ""
		out[i] = e
	}
	return out, nil
}
func (f *fakeBackend) EmailHTML(id string) (string, error) {
	f.htmlReads++
	for _, e := range f.emails {
		if e.ID == id {
			return e.HTMLBody, nil
		}
	}
	return "", nil
}
func (f *fakeBackend) SyncEmails(e []core.Email) error { f.saved += len(e); return nil }
func (f *fakeBackend) DeleteEmails([]string) error     { return nil }
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
		t.Fatalf("%s 应打开写信", shortcut("N"))
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

// 验证进行中关掉弹窗再开，之前那次的结果不能写回来污染新表单。
func TestAddCancelDiscardsStaleResult(t *testing.T) {
	a, _ := newTestApp()

	// 模拟后台还在跑：让 AddAccount 的返回值延迟到下次 post 才回来
	pending := make(chan func(), 1)
	a.post = func(fn func()) { pending <- fn }
	a.async = true
	defer func() { a.post, a.async = func(fn func()) { fn() }, false }()

	a.addOpen = true
	a.add.Email = "new@163.com"
	a.add.Password = "code"
	a.SubmitAdd()
	if !a.add.Busy {
		t.Fatal("SubmitAdd 应立刻进入 busy")
	}

	a.CancelAdd()
	if a.addOpen || a.add.Busy || a.add.Email != "" {
		t.Fatalf("取消应清掉表单：open=%v busy=%v email=%q", a.addOpen, a.add.Busy, a.add.Email)
	}

	// 后台那次调用的 done 回调现在才跑回来——因为 gen 已经变了，结果应被丢弃
	(<-pending)()
	if a.add.Busy || a.add.Err != "" || len(a.accounts) != 1 {
		t.Fatalf("过期结果不应写回：busy=%v err=%q accounts=%d", a.add.Busy, a.add.Err, len(a.accounts))
	}
	// f.added 记录的是后台那次 AddAccount 确实被调用了，不代表界面合并了它；
	// 界面的 accounts 不变就够了。
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

func TestMailDocumentThemeAndCSP(t *testing.T) {
	doc, remote := MailDocument(`<div style="color:red">x</div><img src="https://t.example/p.gif"><script>alert(1)</script>`, BodyThemeFor(velaTheme(true)), MailHeader{Subject: "主题<b>"})
	for _, want := range []string{"style-src 'unsafe-inline'", "script-src 'none'", "img-src data: cid:", "#141210", "#eae3d4", "#f2611d", `style="color:red"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("文档缺少 %q", want)
		}
	}
	if strings.Contains(doc, "<script") || strings.Contains(MailCSP, "img-src *") {
		t.Error("脚本或远程图片未被阻止")
	}
	if !remote {
		t.Error("应识别出远程图片")
	}
	light, remote2 := MailDocument("<p>x</p>", BodyThemeFor(velaTheme(false)), MailHeader{})
	if !strings.Contains(light, "#f7f4ea") || remote2 {
		t.Error("浅色主题背景不对或误报远程图片")
	}
}

type failBody struct{ shows int }

func (b *failBody) Show(string, string, string) bool { b.shows++; return false }
func (b *failBody) Native() ui.NativeWebView         { return nil }

// WebView 无法显示（或加载失败）时，正文用原生纯文本显示，不能是空白。
func TestHTMLBodyFallsBackToText(t *testing.T) {
	a, f := newTestApp()
	f.emails[1].HTMLBody = "<p>HTML 正文段落</p>"
	f.emails[1].Body = ""
	a.emails, _ = f.GetEmails()
	fb := &failBody{}
	a.body = fb
	tt := ui.NewTester(a.View, 1280, 820)
	if err := tt.Click("信用卡账单"); err != nil {
		t.Fatal(err)
	}
	if fb.shows == 0 || !tt.HasText("HTML 正文段落") {
		t.Fatalf("应回退为纯文本：%q", tt.Texts())
	}
	a.body = nil
	a.MarkBodyFailed(a.selected)
	tt.Frame()
	if !tt.HasText("HTML 正文段落") {
		t.Fatal("标记失败后应显示纯文本")
	}
}

// 正文底色必须与阅读区实际使用的主题色完全相同（浅色、深色都检查）。
func TestBodyColorsMatchPaneToken(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := velaTheme(dark)
		bt := BodyThemeFor(th)
		pane := cssColor(th.Background)
		if bt.Bg != pane {
			t.Fatalf("dark=%v 正文底色 %s ≠ 阅读区 %s", dark, bt.Bg, pane)
		}
		doc, _ := MailDocument("<p>x</p>", bt, MailHeader{Subject: "S"})
		for _, want := range []string{"background:" + pane + "!important", "margin:0!important", "scrollbar-color:" + cssColor(th.Scrollbar) + " transparent", "::-webkit-scrollbar-track"} {
			if !strings.Contains(doc, want) {
				t.Errorf("dark=%v 文档缺少 %q", dark, want)
			}
		}
		if !strings.Contains(WebViewBackground(), pane) {
			t.Errorf("WebView 默认底色 %s 不含 %s", WebViewBackground(), pane)
		}
	}
}

type recBody struct{ bg, doc string }

func (b *recBody) Show(_, doc, bg string) bool { b.bg, b.doc = bg, doc; return false }
func (b *recBody) Native() ui.NativeWebView    { return nil }

// 界面交给 WebView 的底色就是这一帧阅读区的底色，抬头也在文档里（与正文一起滚动）。
func TestViewPassesPaneColorAndHeader(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		a, f := newTestApp()
		a.settings.Theme = theme
		f.emails[1].HTMLBody = "<p>正文</p>"
		a.emails, _ = f.GetEmails()
		rb := &recBody{}
		a.body = rb
		tt := ui.NewTester(a.View, 1280, 820)
		_ = tt.Click("信用卡账单")
		want := cssColor(velaTheme(theme == "dark").Background)
		if rb.bg != want || !strings.Contains(rb.doc, "<h1>信用卡账单</h1>") || !strings.Contains(rb.doc, "bank@x.com") {
			t.Fatalf("%s: bg=%s want %s；抬头未进入文档", theme, rb.bg, want)
		}
	}
}

// 点「显示图片」后：文档改用放行远程图片的 CSP，并且保持居中。
func TestShowImagesReloadsDocument(t *testing.T) {
	a, _ := newTestApp()
	src := `<p>x</p><img src="https://t.example/p.gif">`
	th := BodyThemeFor(velaTheme(false))
	h := MailHeader{Subject: "S"}
	doc, _ := a.mailDocument("m1", src, th, h)
	if !strings.Contains(doc, "已阻止远程图片") || !strings.Contains(doc, MailCSP) {
		t.Fatalf("默认应阻止远程图片：%s", doc)
	}
	a.ShowImages("m1")
	doc, _ = a.mailDocument("m1", src, th, h)
	if !strings.Contains(doc, MailCSPImages) || strings.Contains(doc, MailCSP+"\">") {
		t.Fatalf("显示图片后应放行远程图片：%s", doc)
	}
	if !strings.Contains(doc, "已加载远程图片") {
		t.Fatalf("应提示图片已加载：%s", doc)
	}
	// 抬头与正文居中，最大化窗口时不会挤在左边
	if !strings.Contains(doc, ".vela-body{max-width:820px;margin:0 auto") {
		t.Fatal("正文应居中")
	}
}
