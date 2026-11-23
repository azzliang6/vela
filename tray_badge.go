package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// withUnreadDot 在托盘图标右上角画一个红点，表示有未读邮件。
// Windows 和多数 Linux 托盘没有数字角标，红点是三个平台都能看到的提示。
// 解码失败时原样返回。
func withUnreadDot(src []byte) []byte {
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		return src
	}
	b := img.Bounds()
	out := image.NewNRGBA(b)
	draw.Draw(out, b, img, b.Min, draw.Src)

	w := b.Dx()
	r := float64(w) * 0.22  // 红点半径
	ring := float64(w) / 16 // 白色描边，深浅色托盘上都清楚
	cx := float64(b.Max.X) - r - ring
	cy := float64(b.Min.Y) + r + ring
	red := color.NRGBA{0xE5, 0x48, 0x2D, 0xFF}
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d2 := dx*dx + dy*dy
			switch {
			case d2 <= r*r:
				out.SetNRGBA(x, y, red)
			case d2 <= (r+ring)*(r+ring):
				out.SetNRGBA(x, y, white)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return src
	}
	return buf.Bytes()
}
