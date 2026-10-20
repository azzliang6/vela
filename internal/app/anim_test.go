package app

import (
	"testing"
	"time"

	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo/ui"
)

// 动画结束后界面不再请求新帧：空闲时 CPU 应接近 0。
func TestIdleAfterAnimations(t *testing.T) {
	a, _ := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	_ = tt.Click("信用卡账单") // 详情滑入
	_ = tt.Click("回复")    // 对话框淡入
	tt.Advance(time.Second)
	if tt.FramePending() {
		t.Fatal("动画结束后仍在请求帧（存在空闲动画循环）")
	}
}

// 新邮件高亮在结束时只调度一帧，之后不再请求帧，高亮也会被清除。
func TestArrivalHighlightExpires(t *testing.T) {
	a, _ := newTestApp()
	clock := time.Date(2026, 10, 20, 9, 0, 0, 0, time.Local)
	a.now = func() time.Time { return clock }
	tt := ui.NewTester(a.View, 1280, 820)
	a.OnNewMail(a.accounts[0].ID, []core.Email{{ID: "new-1", AccountID: a.accounts[0].ID, Folder: "inbox",
		Subject: "新邮件到了", IsNewArrival: true, Date: clock.Format(time.RFC3339)}})
	tt.Frame()
	if !tt.HasText("新邮件到了") || a.fresh["new-1"].IsZero() {
		t.Fatal("新邮件应显示并处于高亮状态")
	}
	clock = clock.Add(freshDur + time.Second)
	tt.Advance(freshDur + time.Second)
	tt.Frame()
	if _, ok := a.fresh["new-1"]; ok {
		t.Fatal("高亮到期后应被清除")
	}
	tt.Advance(time.Second)
	if tt.FramePending() {
		t.Fatal("高亮结束后仍在请求帧")
	}
}

// 开启“减少动态效果”时界面照常工作，且没有动画帧。
func TestReduceMotion(t *testing.T) {
	a, _ := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tt.Click("信用卡账单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("回复"); err != nil {
		t.Fatal(err)
	}
	if tt.FramePending() {
		t.Fatal("减少动态效果时不应有动画帧")
	}
	if !a.composeOpen || a.compose.Subject != "Re: 信用卡账单" {
		t.Fatal("写信对话框应立即出现")
	}
}
