package core

import (
	"bytes"
	"io"
	"mime"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // GBK / GB18030 / Big5 等中文编码
	"github.com/emersion/go-message/mail"
)

// ParseMessage 解析一封原始邮件（对应 Rust imap::parse_message）。
func ParseMessage(raw []byte, account, serverFolder string, uid uint32, seen, flagged bool, remoteName string) (Email, error) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if r == nil {
		return Email{}, err
	}
	h := r.Header
	e := Email{
		ID: EmailID(account, serverFolder, uid), AccountID: account, Folder: serverFolder,
		UID: uid, ServerFolder: serverFolder, RemoteName: remoteName, Real: true,
		Read: seen, Starred: flagged, ThreadCount: 1, Attachments: []Attachment{},
	}
	if from, _ := h.AddressList("From"); len(from) > 0 {
		e.From = Contact{Name: displayName(from[0].Name, from[0].Address), Email: from[0].Address}
	} else {
		e.From = Contact{Name: "未知发件人"}
	}
	conv := func(key string) []Contact {
		list, _ := h.AddressList(key)
		var out []Contact
		for _, a := range list {
			out = append(out, Contact{Name: displayName(a.Name, a.Address), Email: a.Address})
		}
		return out
	}
	e.To, e.CC = conv("To"), conv("Cc")
	if e.To == nil {
		e.To = []Contact{}
	}
	if subj, err := h.Subject(); err == nil && subj != "" {
		e.Subject = fixEncodedWords(subj)
	} else {
		e.Subject = "（无主题）"
	}
	if mid, err := h.MessageID(); err == nil && mid != "" {
		e.MessageID = mid // 与 Rust mail-parser 一致：不带尖括号
	}
	// 常见非 RFC 5322 格式也尝试一下；都不行就留空，让调用方用 IMAP INTERNALDATE
	// 兜底（那是邮件到达服务器的时间），而不是同步时间——否则历史邮件会挤到列表最上面。
	if d, err := h.Date(); err == nil && !d.IsZero() {
		e.Date = d.Format(time.RFC3339)
	} else if raw := strings.TrimSpace(h.Get("Date")); raw != "" {
		for _, layout := range []string{
			"January 2, 2006 15:04:05 -0700", "2006-01-02 15:04:05", "2-Jan-2006 15:04:05 -0700",
			"Mon Jan 2 15:04:05 2006",
		} {
			if d, err := time.Parse(layout, raw); err == nil && !d.IsZero() {
				e.Date = d.Format(time.RFC3339)
				break
			}
		}
	}

	var text, html string
	for {
		p, err := r.NextPart()
		if err == io.EOF || p == nil {
			break
		}
		if err != nil && p == nil {
			break
		}
		switch ph := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := ph.ContentType()
			b, _ := io.ReadAll(p.Body)
			if ct == "text/html" && html == "" {
				html = string(b)
			} else if (ct == "text/plain" || ct == "") && text == "" {
				text = string(b)
			}
		case *mail.AttachmentHeader:
			name, _ := ph.Filename()
			if name == "" {
				name = "未命名附件"
			}
			ct, _, _ := ph.ContentType()
			b, _ := io.ReadAll(p.Body)
			e.Attachments = append(e.Attachments, Attachment{
				ID:   e.ID + ":attachment:" + itoa(len(e.Attachments)),
				Name: name, SizeKB: len(b) / 1024, Kind: AttachmentKind(name, ct),
			})
		}
	}
	if text == "" && html != "" {
		text = HTMLToText(html)
	}
	e.Body = truncateChars(text, MaxBodyChars)
	if html != "" {
		e.HTMLBody = truncateChars(html, MaxBodyChars)
	}
	return e, nil
}

func itoa(i int) string { return strconv.Itoa(i) }

// encodedWordRe 匹配 RFC 2047 编码词（=?charset?B|Q?payload?=）。
var encodedWordRe = regexp.MustCompile(`=\?([^?]+)\?([BbQq])\?([^?]*)\?=`)

// displayName 返回可读的发件人/收件人名字。
// 标准库解码失败（如 QQ 邮箱部分邮件的名字是长度不合法的 base64）时会原样保留
// 编码词，界面上就成了「=?utf-8?B?UVhp…?=」。这里再宽松解码一次，仍解不出来
// 就退回显示邮箱地址。
func displayName(name, addr string) string {
	name = fixEncodedWords(strings.TrimSpace(name))
	if name == "" || encodedWordRe.MatchString(name) || !utf8.ValidString(name) {
		name = addr
	}
	if name == "" {
		name = "未知发件人"
	}
	return name
}

// fixEncodedWords 尽力解码 Unicode 编码词：base64 部分补齐填充后再交给标准
// 解码器；解出的字节不是合法 UTF-8 时丢弃该编码词。
func fixEncodedWords(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	dec := &mime.WordDecoder{CharsetReader: message.CharsetReader}
	return encodedWordRe.ReplaceAllStringFunc(s, func(w string) string {
		m := encodedWordRe.FindStringSubmatch(w)
		charset, enc, payload := m[1], strings.ToUpper(m[2]), m[3]
		if enc == "B" {
			payload = strings.TrimRight(payload, "=")
			if r := len(payload) % 4; r > 0 {
				payload += strings.Repeat("=", 4-r)
			}
		}
		out, err := dec.Decode("=?" + charset + "?" + enc + "?" + payload + "?=")
		if err != nil || !utf8.ValidString(out) {
			return ""
		}
		return out
	})
}

// AttachmentKind 按扩展名（其次 MIME 类型）判断附件图标类别。
func AttachmentKind(name, contentType string) string {
	switch strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".") {
	case "pdf":
		return "pdf"
	case "xls", "xlsx", "csv", "ods":
		return "sheet"
	case "doc", "docx", "odt", "rtf":
		return "doc"
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "svg", "heic":
		return "image"
	case "zip", "rar", "7z", "tar", "gz":
		return "zip"
	case "js", "ts", "java", "py", "rs", "go", "c", "cpp", "json", "xml", "html", "css":
		return "code"
	}
	if mt, _, _ := mime.ParseMediaType(contentType); strings.HasPrefix(mt, "image/") || strings.HasPrefix(contentType, "image/") {
		return "image"
	}
	return "doc"
}

// HTMLToText 把 HTML 粗略转成纯文本（剔除 script/style，块级标签换行）。
func HTMLToText(html string) string {
	var out strings.Builder
	inTag := false
	skip := 0
	lower := strings.ToLower(html)
	for i, c := range html {
		if c == '<' {
			rest := lower[i:]
			switch {
			case strings.HasPrefix(rest, "<script"), strings.HasPrefix(rest, "<style"):
				skip++
			case strings.HasPrefix(rest, "</script"), strings.HasPrefix(rest, "</style"):
				if skip > 0 {
					skip--
				}
			}
			for _, p := range []string{"<br", "</p", "</div", "</tr", "</h1", "</h2", "</h3", "</li"} {
				if strings.HasPrefix(rest, p) {
					out.WriteByte('\n')
					break
				}
			}
			inTag = true
			continue
		}
		if c == '>' {
			inTag = false
			continue
		}
		if !inTag && skip == 0 {
			out.WriteRune(c)
		}
	}
	decoded := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"", "&#39;", "'").Replace(out.String())
	var res strings.Builder
	blank := 0
	for _, line := range strings.Split(decoded, "\n") {
		t := strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(t) == "" {
			blank++
			if blank > 1 {
				continue
			}
			res.WriteByte('\n')
		} else {
			blank = 0
			res.WriteString(t)
			res.WriteByte('\n')
		}
	}
	return strings.TrimSpace(res.String())
}

func truncateChars(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	n := 0
	for i := range s {
		if n == max {
			return s[:i] + "\n\n……（正文过长已截断）"
		}
		n++
	}
	return s
}
