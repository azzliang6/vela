package core

import (
	"bytes"
	"io"
	"mime"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
		e.From = Contact{Name: from[0].Name, Email: from[0].Address}
	} else {
		e.From = Contact{Name: "未知发件人"}
	}
	if e.From.Name == "" {
		e.From.Name = e.From.Email
	}
	conv := func(key string) []Contact {
		list, _ := h.AddressList(key)
		var out []Contact
		for _, a := range list {
			n := a.Name
			if n == "" {
				n = a.Address
			}
			out = append(out, Contact{Name: n, Email: a.Address})
		}
		return out
	}
	e.To, e.CC = conv("To"), conv("Cc")
	if e.To == nil {
		e.To = []Contact{}
	}
	if subj, err := h.Subject(); err == nil && subj != "" {
		e.Subject = subj
	} else {
		e.Subject = "（无主题）"
	}
	if mid, err := h.MessageID(); err == nil && mid != "" {
		e.MessageID = mid // 与 Rust mail-parser 一致：不带尖括号
	}
	if d, err := h.Date(); err == nil && !d.IsZero() {
		e.Date = d.Format(time.RFC3339)
	} else {
		e.Date = time.Now().UTC().Format(time.RFC3339)
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
