package app

import (
	"strings"
	"testing"

	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo/ui"
)

func TestContainsFold(t *testing.T) {
	cases := []struct {
		s, q string
		want bool
	}{
		{"Quarterly REPORT", "report", true},
		{"Quarterly REPORT", "rep0rt", false},
		{"项目周报 第 9 期", "周报 9", false},
		{"项目周报 9", "周报 9", true},
		{"Hello, Wörld", "wörld", true},
		{"ÉCOLE", "école", true},
		{"abc", "abcd", false},
		{"", "a", false},
		{"anything", "", true},
		{"xxRxxreport", "report", true}, // 首字母大写形式先出现、但不是匹配的位置
		{"Re: 账单 Bill", "bill", true},
	}
	for _, c := range cases {
		if got := containsFold(c.s, c.q); got != c.want {
			t.Errorf("containsFold(%q, %q) = %v，期望 %v", c.s, c.q, got, c.want)
		}
		// 与最直接的写法结果一致
		if ref := strings.Contains(strings.ToLower(c.s), c.q); ref != c.want {
			t.Errorf("用例本身有误：%q %q", c.s, c.q)
		}
	}
}

func TestPreviewTextTruncatesOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("中文 ", 400)
	p := previewText(long)
	if len(p) > previewMaxBytes || !strings.HasPrefix(p, "中文 中文") || strings.ContainsRune(p, '�') {
		t.Fatalf("摘要应截断在字符边界：%d %q", len(p), p[len(p)-10:])
	}
}

// 过滤结果被缓存，但星标、移动、搜索词变化后必须立刻反映出来。
func TestVisibleCacheInvalidates(t *testing.T) {
	a, _ := newTestApp()
	if n := len(a.Visible()); n != 2 {
		t.Fatalf("收件箱应有 2 封：%d", n)
	}
	a.folder = "starred"
	if n := len(a.Visible()); n != 0 {
		t.Fatalf("还没有星标邮件：%d", n)
	}
	id := a.emails[0].ID
	a.ToggleStar(id)
	if v := a.Visible(); len(v) != 1 || v[0].ID != id {
		t.Fatalf("加星后星标列表应立即出现：%d", len(v))
	}
	a.folder = "inbox"
	a.query = "账单"
	if v := a.Visible(); len(v) != 1 || v[0].Subject != "信用卡账单" {
		t.Fatalf("搜索：%v", v)
	}
	a.query = "账单x" // 变长：在上次命中里继续筛
	if v := a.Visible(); len(v) != 0 {
		t.Fatalf("更长的词不应再命中：%d", len(v))
	}
	a.query = "纪要" // 换词：重新完整扫描
	if v := a.Visible(); len(v) != 1 || v[0].Subject != "周会纪要" {
		t.Fatalf("换词后：%v", v)
	}
	a.Move(v0(a).ID, "archive")
	if v := a.Visible(); len(v) != 0 {
		t.Fatalf("归档后收件箱搜索不应再命中：%d", len(v))
	}
}

func v0(a *App) *core.Email { return a.Visible()[0] }

// HTML 正文只在打开邮件时读取一次，之后走缓存；新同步的邮件内存里不留 HTML。
func TestHTMLBodyLoadsOnDemand(t *testing.T) {
	a, f := newTestApp()
	f.emails[1].HTMLBody = "<p>按需加载的正文</p>"
	a.emails, _ = f.GetEmails()
	if a.emails[1].HTMLBody != "" || !a.emails[1].HasHTML {
		t.Fatal("列表里的邮件不应带 HTML 正文")
	}
	rb := &recBody{}
	a.body = rb
	tt := ui.NewTester(a.View, 1280, 820)
	if f.htmlReads != 0 {
		t.Fatal("没打开邮件之前不应读取 HTML")
	}
	_ = tt.Click("信用卡账单")
	tt.Frame()
	tt.Frame()
	if f.htmlReads != 1 || !strings.Contains(rb.doc, "按需加载的正文") {
		t.Fatalf("打开时应只读取一次 HTML：reads=%d", f.htmlReads)
	}

	// 同步收到的新邮件：写库时带 HTML，内存里只留标记，打开时走缓存不读库
	f.sync = map[string]core.SyncOutcome{"me@qq.com": {AccountID: "me@qq.com", Emails: []core.Email{{
		ID: "me@qq.com:inbox:30", AccountID: "me@qq.com", Folder: "inbox", Subject: "新到的富文本",
		HTMLBody: "<p>新邮件 HTML</p>", Date: "2026-10-20T10:00:00+08:00", Real: true, UID: 30}}}}
	a.Sync()
	_, e := a.find("me@qq.com:inbox:30")
	if e == nil || e.HTMLBody != "" || !e.HasHTML {
		t.Fatalf("新邮件内存里不应保留 HTML：%+v", e)
	}
	if s, ok := a.htmlFor(e); !ok || s != "<p>新邮件 HTML</p>" || f.htmlReads != 1 {
		t.Fatalf("新邮件的 HTML 应已在缓存里：%q reads=%d", s, f.htmlReads)
	}
}
