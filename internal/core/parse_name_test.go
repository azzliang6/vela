package core

import "testing"

func TestDisplayNameDecodesLenientBase64(t *testing.T) {
	// QQ 邮箱会发这种长度不合法的 base64 编码词，标准库解不了
	got := displayName("=?utf-8?B?UVXpg7q7nnrHlim6LpmJ8=?=", "10000@qq.com")
	if got != "10000@qq.com" {
		t.Fatalf("解不出来应退回邮箱地址：%q", got)
	}
	// 正常的编码词仍应解出中文
	if got := displayName("=?utf-8?B?5L2g5aW9?=", "a@b.com"); got != "你好" {
		t.Fatalf("正常编码词应解码：%q", got)
	}
	// 纯文本名字原样保留
	if got := displayName("Claude Team", "a@b.com"); got != "Claude Team" {
		t.Fatalf("纯文本名字不应改动：%q", got)
	}
	// 名字为空时用邮箱地址
	if got := displayName("", "a@b.com"); got != "a@b.com" {
		t.Fatalf("空名字应退回邮箱：%q", got)
	}
}
