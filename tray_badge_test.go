package main

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

func TestUnreadDot(t *testing.T) {
	out := withUnreadDot(trayIcon)
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := png.Decode(bytes.NewReader(trayIcon))
	if img.Bounds() != orig.Bounds() {
		t.Fatalf("尺寸应不变：%v → %v", orig.Bounds(), img.Bounds())
	}
	w := img.Bounds().Dx()
	r, g, _, a := img.At(w-w/4, w/4).RGBA()
	if a>>8 != 0xFF || r>>8 < 0xC0 || g>>8 > 0x80 {
		t.Fatalf("右上角应是红点：%x %x %x", r>>8, g>>8, a>>8)
	}
	if p := os.Getenv("VELA_SHOTS"); p != "" {
		_ = os.WriteFile(p+"/tray-unread.png", out, 0o644)
	}
	if got := withUnreadDot([]byte("not png")); string(got) != "not png" {
		t.Fatal("解码失败应原样返回")
	}
}
