package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func drafts(a *App) []string {
	var out []string
	for _, e := range a.emails {
		if e.Folder == "drafts" {
			out = append(out, e.ID)
		}
	}
	return out
}

// 存草稿 → 继续编辑 → 再存：同一封草稿原地更新，收件人、抄送、回复目标都保留。
func TestDraftEditInPlace(t *testing.T) {
	a, _ := newTestApp()
	a.Select(a.emails[1].ID)
	a.StartCompose("reply")
	a.compose.CC = "boss@x.com"
	a.compose.Body = "第一版" + a.compose.Body
	a.SaveDraft()
	ds := drafts(a)
	if len(ds) != 1 || a.composeOpen {
		t.Fatalf("应存下一封草稿并关闭窗口：%v", ds)
	}

	a.OpenDraft(ds[0])
	c := a.compose
	if c == nil || c.DraftID != ds[0] || c.To != "bank@x.com" || c.CC != "boss@x.com" ||
		c.InReplyTo == "" || c.Mode != "reply" || !strings.HasPrefix(c.Body, "第一版") {
		t.Fatalf("继续编辑应还原全部字段：%+v", c)
	}
	c.Body = "第二版"
	a.SaveDraft()
	if ds2 := drafts(a); len(ds2) != 1 || ds2[0] != ds[0] {
		t.Fatalf("再存应原地更新而不是新建：%v", ds2)
	}
	if _, e := a.find(ds[0]); e.Body != "第二版" || e.CC[0].Email != "boss@x.com" {
		t.Fatalf("草稿内容未更新：%+v", e)
	}
}

// 发送一封从草稿继续编辑的邮件后，草稿从草稿箱移除。
func TestSendingDraftRemovesIt(t *testing.T) {
	a, f := newTestApp()
	a.StartCompose("new")
	a.compose.To, a.compose.Subject = "x@y.com", "打个招呼"
	a.SaveDraft()
	id := drafts(a)[0]
	a.OpenDraft(id)
	a.SendCompose()
	if len(f.sent) != 1 || f.sent[0].Subject != "打个招呼" {
		t.Fatalf("应发送草稿内容：%+v", f.sent)
	}
	if len(drafts(a)) != 0 {
		t.Fatal("寄出后草稿应移除")
	}
}

// 直接关掉写了一半的信会自动存草稿；没改动或点了「舍弃」则不存。
func TestComposeAutoSavesOnClose(t *testing.T) {
	a, f := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)

	a.StartCompose("new")
	tt.Frame()
	a.composeOpen = false // 相当于 Esc / 点遮罩 / 关闭按钮
	tt.Frame()
	if len(drafts(a)) != 0 || len(a.toasts) != 0 {
		t.Fatal("什么都没写，关闭时不应存草稿")
	}

	a.StartCompose("new")
	a.compose.Subject = "写了一半"
	tt.Frame()
	a.composeOpen = false
	tt.Frame()
	if len(drafts(a)) != 1 || a.compose != nil || !strings.Contains(strings.Join(a.toasts, ""), "自动存为草稿") {
		t.Fatalf("关闭时应自动存草稿：%v %v", drafts(a), a.toasts)
	}

	saved := f.saved
	a.OpenDraft(drafts(a)[0])
	tt.Frame()
	a.composeOpen = false // 打开草稿没改动就关掉
	tt.Frame()
	if len(drafts(a)) != 1 || f.saved != saved {
		t.Fatalf("没有改动时不应重复保存：写库 %d → %d 次", saved, f.saved)
	}

	a.StartCompose("new")
	a.compose.Subject = "不要了"
	tt.Frame()
	if err := tt.Click("舍弃"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(drafts(a)) != 1 {
		t.Fatalf("舍弃不应存草稿：%v", drafts(a))
	}
}

// 回复时原样带出的引用不算改动：不动一个字就关掉，不留草稿。
func TestUntouchedReplyIsNotSaved(t *testing.T) {
	a, _ := newTestApp()
	a.Select(a.emails[0].ID)
	a.StartCompose("reply")
	a.composeOpen = false
	a.composeClosed()
	if len(drafts(a)) != 0 {
		t.Fatal("未改动的回复不应存草稿")
	}
}

// 草稿的阅读区有「继续编辑」按钮。
func TestDraftViewerHasEditButton(t *testing.T) {
	a, _ := newTestApp()
	a.StartCompose("new")
	a.compose.Subject = "未完成的草稿"
	a.SaveDraft()
	a.folder = "drafts"
	tt := ui.NewTester(a.View, 1280, 820)
	if err := tt.Click("未完成的草稿"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("继续编辑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.composeOpen || a.compose.Subject != "未完成的草稿" {
		t.Fatal("点继续编辑应打开写信窗口")
	}
}

// 未读数变化时通知宿主一次，没变化不重复通知。
func TestUnreadHook(t *testing.T) {
	a, _ := newTestApp()
	var got []int
	a.SetUnreadHook(func(n int) { got = append(got, n) })
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	tt.Frame()
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("启动后应通知一次未读数 1：%v", got)
	}
	_ = tt.Click("信用卡账单") // 打开即已读
	tt.Frame()
	if len(got) != 2 || got[1] != 0 {
		t.Fatalf("读完后应通知 0：%v", got)
	}
	a.ToggleStar(a.emails[0].ID) // 与未读无关的改动
	tt.Frame()
	if len(got) != 2 {
		t.Fatalf("未读数没变不应重复通知：%v", got)
	}
}
