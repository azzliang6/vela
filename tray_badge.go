package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
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
	// 红点只作提示，画小一点：半径 13%，白描边约 1.5%——原来 22%/6% 在 16px
	// 的托盘里几乎盖住半个图标。
	r := float64(w) * 0.13
	ring := math.Max(1, float64(w)*0.015)
	cx := float64(b.Max.X) - r - ring*1.5
	cy := float64(b.Min.Y) + r + ring*1.5
	red := color.NRGBA{0xE5, 0x48, 0x2D, 0xFF}
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := math.Sqrt(dx*dx + dy*dy)
			// 按像素覆盖比例混合，边缘不会有锯齿
			if cov := clamp01(r + ring + 0.5 - d); cov > 0 {
				blend(out, x, y, white, cov)
			}
			if cov := clamp01(r + 0.5 - d); cov > 0 {
				blend(out, x, y, red, cov)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return src
	}
	return buf.Bytes()
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// blend 把 src 以 alpha 覆盖在 out 的 (x,y) 上。
func blend(out *image.NRGBA, x, y int, src color.NRGBA, a float64) {
	dst := out.NRGBAAt(x, y)
	na := a + float64(dst.A)/255*(1-a)
	if na == 0 {
		return
	}
	mix := func(s, d uint8) uint8 {
		v := (float64(s)*a + float64(d)*float64(dst.A)/255*(1-a)) / na
		return uint8(math.Round(v))
	}
	out.SetNRGBA(x, y, color.NRGBA{mix(src.R, dst.R), mix(src.G, dst.G), mix(src.B, dst.B), uint8(math.Round(na * 255))})
}
