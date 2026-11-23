package app

import (
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 快捷键提示要与实际按键一致：macOS 上是 ⌘，其它平台是 Ctrl（见 ui.Cmd）。
func TestShortcutHintsMatchPlatform(t *testing.T) {
	darwin := runtime.GOOS == "darwin"
	if want := map[bool]string{true: "⌘N", false: "Ctrl+N"}[darwin]; shortcut("N") != want {
		t.Fatalf("shortcut(N) = %q，应为 %q", shortcut("N"), want)
	}

	a, _ := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)
	for _, want := range []string{shortcut("N"), shortcut("K")} {
		if !tt.HasText(want) {
			t.Fatalf("界面上应显示 %q：%q", want, tt.Texts())
		}
	}
	for _, s := range tt.Texts() {
		if strings.Contains(s, "⌘") != darwin {
			t.Fatalf("快捷键提示 %q 与平台不符", s)
		}
	}
}
