package main

import (
	"strings"
	"testing"
	"time"

	"github.com/azzliang6/vela/internal/app"
)

type fakePage struct{ html, base []string }

func (p *fakePage) LoadHTML(h, b string) { p.html = append(p.html, h); p.base = append(p.base, b) }

func newBody() (*webBody, *fakePage, *[]string) {
	p := &fakePage{}
	failed := &[]string{}
	b := &webBody{page: p, setBg: func(string) error { return nil }, timeout: 20 * time.Millisecond,
		post: func(f func()) { f() }}
	b.onFail = func(id string) { *failed = append(*failed, id) }
	return b, p, failed
}

// 回归：Windows WebView2 下 baseURL 非空（about:blank）会导致正文空白，必须传空。
func TestWebBodyLoadsWithEmptyBaseURL(t *testing.T) {
	b, p, _ := newBody()
	if !b.Show("m1|#141210", "<p>hi</p>", "#141210") {
		t.Fatal("应能显示")
	}
	b.Show("m1|#141210", "<p>hi</p>", "#141210") // 同一 key 不重复加载
	if len(p.base) != 1 || p.base[0] != "" {
		t.Fatalf("LoadHTML baseURL = %q，应只调用一次且为空", p.base)
	}
}

func TestWebBodyTimeoutFallsBack(t *testing.T) {
	b, _, failed := newBody()
	mu := make(chan func(), 4)
	b.post = func(f func()) { mu <- f }
	b.Show("m1|x", "<p>a</p>", "x")
	(<-mu)()
	if len(*failed) != 1 || (*failed)[0] != "m1" {
		t.Fatalf("超时未加载应回退纯文本：%v", *failed)
	}
	// 已加载则不回退
	b.Show("m2|x", "<p>b</p>", "x")
	b.loaded = true
	(<-mu)()
	if len(*failed) != 1 {
		t.Fatalf("加载完成后不应回退：%v", *failed)
	}
}

func TestWebBodyRejectsHugeDoc(t *testing.T) {
	b, p, _ := newBody()
	if b.Show("big|x", strings.Repeat("a", app.MaxDocBytes+1), "x") || len(p.html) != 0 {
		t.Fatal("超过 NavigateToString 上限的文档应改用纯文本")
	}
}
