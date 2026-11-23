package app

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// containsFold 判断 s 是否包含 q，忽略大小写；q 必须已经是小写。
// 搜索每帧要扫过全部邮件正文，这里不分配内存：先用 IndexByte 定位首字节的
// 大小写两种写法，再用 EqualFold 比对整个词。中文等没有大小写的词直接走
// strings.Contains。
func containsFold(s, q string) bool {
	if q == "" {
		return true
	}
	if len(s) < len(q) {
		return false
	}
	if !hasCase(q) {
		return strings.Contains(s, q)
	}
	r, _ := utf8.DecodeRuneInString(q)
	if r >= utf8.RuneSelf {
		// 首字符是有大小写的非 ASCII 字母（如 É、Ж）：少见，走简单可靠的方式
		return strings.Contains(strings.ToLower(s), q)
	}
	lo, up := byte(unicode.ToLower(r)), byte(unicode.ToUpper(r))
	for i := 0; i+len(q) <= len(s); {
		j := indexEither(s[i:], lo, up)
		if j < 0 {
			return false
		}
		i += j
		if i+len(q) <= len(s) && strings.EqualFold(s[i:i+len(q)], q) {
			return true
		}
		i++
	}
	return false
}

// hasCase 报告 q 里是否有区分大小写的字符。
func hasCase(q string) bool {
	for _, r := range q {
		if r < utf8.RuneSelf {
			if 'a' <= r && r <= 'z' {
				return true
			}
			continue
		}
		if unicode.SimpleFold(r) != r {
			return true
		}
	}
	return false
}

// indexEither 返回 a 或 b 在 s 中第一次出现的位置。
func indexEither(s string, a, b byte) int {
	i := strings.IndexByte(s, a)
	if a == b {
		return i
	}
	// 只在已知的 a 位置之前找 b，避免在长正文里来回扫两遍
	lim := s
	if i >= 0 {
		lim = s[:i]
	}
	if j := strings.IndexByte(lim, b); j >= 0 {
		return j
	}
	return i
}

// previewMaxBytes 是列表摘要最多取用的正文字节数：摘要最多显示两行，
// 没必要每帧把整封长信的空白都折叠一遍。
const previewMaxBytes = 600

// previewText 把正文开头折叠成一行摘要。
func previewText(body string) string {
	if len(body) > previewMaxBytes {
		cut := previewMaxBytes
		for cut > 0 && !utf8.RuneStart(body[cut]) {
			cut--
		}
		body = body[:cut]
	}
	return strings.Join(strings.Fields(body), " ")
}
