package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo/ui"
)

// bigApp 造一个有 n 封邮件（正文约 4KB）的应用，用于衡量列表与搜索开销。
func bigApp(n int) *App {
	body := strings.Repeat("这是一段比较长的邮件正文，包含 Lorem ipsum dolor sit amet 等内容。", 50)
	ems := make([]core.Email, n)
	for i := range ems {
		e := mail(fmt.Sprint(i), fmt.Sprintf("第 %d 封：项目周报", i), "Alice", i%3 == 0)
		e.Date = fmt.Sprintf("2026-%02d-%02dT10:%02d:00+08:00", 1+i%12, 1+i%28, i%60)
		e.Body = body
		ems[i] = e
	}
	f := &fakeBackend{accounts: []core.MailboxAccount{{ID: "me@qq.com", Email: "me@qq.com"}}, emails: ems}
	return New(f, "")
}

func BenchmarkVisible(b *testing.B) {
	a := bigApp(20000)
	b.ResetTimer()
	for range b.N {
		a.rev++ // 每轮都让缓存失效，衡量完整扫描的开销
		_ = a.Visible()
	}
}

func BenchmarkVisibleSearch(b *testing.B) {
	a := bigApp(20000)
	a.query = "周报 9"
	b.ResetTimer()
	for range b.N {
		a.rev++ // 每轮都让缓存失效，衡量完整扫描的开销
		_ = a.Visible()
	}
}

// BenchmarkSearchTyping 模拟逐字输入搜索词：每次按键都是一个新的、更长的词。
func BenchmarkSearchTyping(b *testing.B) {
	a := bigApp(20000)
	words := []string{"l", "lo", "lor", "lore", "lorem"}
	b.ResetTimer()
	for range b.N {
		a.rev++
		for _, w := range words {
			a.query = w
			_ = a.Visible()
		}
	}
}

func BenchmarkFrame(b *testing.B) {
	a := bigApp(20000)
	tt := ui.NewTester(a.View, 1280, 820)
	b.ResetTimer()
	for range b.N {
		tt.Frame()
	}
}
