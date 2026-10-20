package app

import (
	"fmt"
	"hash/fnv"
	"html"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// 邮件 HTML 正文的安全处理。
//
// 原 React 版用 <iframe sandbox=""> 显示：禁止脚本、表单和跳转，保留图片和样式。
// 原生版用系统 WebView 显示，这里做同等的限制：
//   - bluemonday 白名单清洗：去掉 script / iframe / object / form / on* 事件属性等
//   - CSP 元标签：script-src 'none'，再兜底一层
//   - 链接跳转由 WebView 的 OnWillNavigate 拦下，改用系统浏览器打开（见 main.go）
// 远程内容（图片、字体、样式表）一律阻止，界面上提示“已阻止远程图片”。

var (
	policyOnce sync.Once
	policy     *bluemonday.Policy
)

func mailPolicy() *bluemonday.Policy {
	policyOnce.Do(func() {
		p := bluemonday.UGCPolicy()
		p.AllowAttrs("style", "class", "align", "valign", "bgcolor", "width", "height", "border",
			"cellpadding", "cellspacing", "color", "face", "size", "dir").Globally()
		p.AllowElements("font", "center", "span", "div", "table", "thead", "tbody", "tfoot", "tr", "td", "th", "img", "style")
		p.AllowAttrs("src", "alt").OnElements("img")
		p.AllowDataURIImages()
		p.AllowURLSchemes("http", "https", "mailto", "cid", "data")
		p.AllowUnsafe(true) // 只为保留 <style> 的 CSS；脚本已被清洗 + CSP 禁止
		p.RequireNoFollowOnLinks(false)
		policy = p
	})
	return policy
}

// BodyTheme 是正文文档的配色，全部取自界面实际使用的主题（velaTheme），
// 不另外硬编码：Bg 就是阅读区的底色 Theme.Background。
type BodyTheme struct {
	Dark                                    bool
	Bg, Text, Muted, Accent, Border, Select string
	Scrollbar                               string
	ScrollbarWidth                          float32
}

// cssColor 把 ui.Color 转成 CSS 颜色。
func cssColor(c ui.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", c.R, c.G, c.B, float64(c.A)/255)
}

// BodyThemeFor 从主题取正文配色。
func BodyThemeFor(t *ui.Theme) BodyTheme {
	w := t.ScrollbarWidth
	if w <= 0 {
		w = 6
	}
	return BodyTheme{Dark: t.Dark, Bg: cssColor(t.Background), Text: cssColor(t.Text), Muted: cssColor(t.TextMuted),
		Accent: cssColor(t.Accent), Border: cssColor(t.Border), Select: cssColor(t.Selection),
		Scrollbar: cssColor(t.Scrollbar), ScrollbarWidth: w}
}

// WebViewBackground 是创建 WebView 时的默认底色（首帧之前），light-dark() 两套都取自主题。
func WebViewBackground() string {
	return "light-dark(" + cssColor(velaTheme(false).Background) + ", " + cssColor(velaTheme(true).Background) + ")"
}

// MailHeader 是渲染进 HTML 文档的抬头，让抬头与正文一起滚动。
type MailHeader struct {
	Subject, FromName, FromEmail, Recipients, Date string
	SummaryTitle, Summary, Why                     string
	Attachments                                    []string
}

// docKey 是文档内容的短哈希，内容（主题、AI 摘要…）变化时才重新加载。
func docKey(doc string) string {
	h := fnv.New64a()
	h.Write([]byte(doc))
	return fmt.Sprintf("%x", h.Sum64())
}

func headerHTML(h MailHeader, remote bool) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<header class="vela-h"><h1>` + esc(h.Subject) + `</h1><div class="vela-from">`)
	initial := strings.ToUpper(firstRune(nonEmpty(h.FromName, h.FromEmail)))
	b.WriteString(`<span class="vela-av">` + esc(initial) + `</span><div class="vela-who"><b>` + esc(h.FromName) +
		`</b>&nbsp; &lt;` + esc(h.FromEmail) + `&gt;<div class="vela-m">` + esc(h.Recipients) + `</div></div>`)
	if h.Date != "" {
		b.WriteString(`<span class="vela-m vela-date">` + esc(h.Date) + `</span>`)
	}
	b.WriteString(`</div>`)
	if h.Summary != "" {
		b.WriteString(`<div class="vela-ai"><div class="vela-ai-t">` + esc(h.SummaryTitle) + `</div>` + esc(h.Summary))
		if h.Why != "" {
			b.WriteString(`<div class="vela-m">` + esc(h.Why) + `</div>`)
		}
		b.WriteString(`</div>`)
	}
	if len(h.Attachments) > 0 {
		b.WriteString(`<div class="vela-att">`)
		for _, a := range h.Attachments {
			b.WriteString(`<span>📎 ` + esc(a) + `</span>`)
		}
		b.WriteString(`</div>`)
	}
	if remote {
		b.WriteString(`<div class="vela-m vela-blocked">已阻止远程图片</div>`)
	}
	b.WriteString(`</header>`)
	return b.String()
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return "?"
}

// MailCSP 是正文文档的内容安全策略：
//   - 禁止一切脚本、表单、框架和网络请求（远程图片、字体、样式表都被阻止）
//   - 允许内联样式（邮件排版几乎全靠 style 属性和 <style>），以及 data:/cid: 图片
const MailCSP = "default-src 'none'; script-src 'none'; object-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'; " +
	"img-src data: cid:; style-src 'unsafe-inline'; font-src data:"

// MaxDocBytes 是交给 WebView 的文档上限。WebView2 的 NavigateToString 限 2MB，
// 超过时界面改用纯文本。
const MaxDocBytes = 1_500_000

var remoteImg = regexp.MustCompile(`(?i)(<img[^>]+src\s*=\s*["']?\s*(https?:)?//)|url\(\s*["']?\s*(https?:)?//`)

// MailDocument 把邮件 HTML 清洗后包成完整文档：抬头 + 正文，注入跟随应用主题的基础样式。
// 外层（html/body）的底色强制等于阅读区底色，邮件内部自己的样式保持不变。
// remote 表示原文引用了被阻止的远程图片。
func MailDocument(src string, th BodyTheme, h MailHeader) (doc string, remote bool) {
	clean := mailPolicy().Sanitize(src)
	remote = remoteImg.MatchString(clean)
	scheme := "light"
	if th.Dark {
		scheme = "dark"
	}
	sw := fmt.Sprintf("%gpx", th.ScrollbarWidth)
	css := "html,body{margin:0!important;padding:0!important;border:0!important;background:" + th.Bg + "!important;color:" + th.Text + "}" +
		"html{scrollbar-width:thin;scrollbar-color:" + th.Scrollbar + " transparent}" +
		"::-webkit-scrollbar{width:" + sw + ";height:" + sw + ";background:transparent}" +
		"::-webkit-scrollbar-track,::-webkit-scrollbar-corner{background:transparent}" +
		"::-webkit-scrollbar-thumb{background:" + th.Scrollbar + ";border-radius:" + sw + "}" +
		"::-webkit-scrollbar-button{display:none}" +
		".vela-wrap{font:14px/1.6 -apple-system,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;word-wrap:break-word;overflow-wrap:anywhere}" +
		".vela-h{padding:20px 28px 12px;font:14px/1.5 -apple-system,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;color:" + th.Text + "}" +
		".vela-h h1{font-size:22px;line-height:1.3;margin:0 0 8px;font-weight:700}" +
		".vela-from{display:flex;align-items:center;gap:10px}.vela-who{flex:1;min-width:0}" +
		".vela-av{flex:none;width:34px;height:34px;border-radius:50%;display:flex;align-items:center;justify-content:center;background:" + th.Select + ";color:" + th.Accent + ";font-weight:700}" +
		".vela-m{font-size:12px;color:" + th.Muted + "}.vela-date{flex:none}" +
		".vela-ai{margin-top:8px;padding:10px 12px;border-radius:8px;background:" + th.Select + "}.vela-ai-t{font-size:12px;font-weight:700;color:" + th.Accent + "}" +
		".vela-att{margin-top:8px;display:flex;flex-wrap:wrap;gap:6px}.vela-att span{font-size:13px;padding:5px 10px;border:1px solid " + th.Border + ";border-radius:8px}" +
		".vela-blocked{margin-top:6px}" +
		".vela-body{padding:4px 28px 28px}.vela-body a{color:" + th.Accent + "}.vela-body img{max-width:100%;height:auto}.vela-body table{max-width:100%}" +
		".vela-body blockquote{margin:0 0 0 4px;padding-left:12px;border-left:3px solid " + th.Border + ";color:" + th.Muted + "}"
	doc = "<!doctype html><html><head><meta charset=\"utf-8\">" +
		"<meta http-equiv=\"Content-Security-Policy\" content=\"" + MailCSP + "\">" +
		"<meta name=\"color-scheme\" content=\"" + scheme + "\">" +
		"<meta name=\"viewport\" content=\"width=device-width\">" +
		"<style>" + css + "</style></head><body><div class=\"vela-wrap\">" + headerHTML(h, remote) +
		"<div class=\"vela-body\">" + clean + "</div></div></body></html>"
	return doc, remote
}

// SanitizeMailHTML 保留旧接口：按主题生成正文文档。
func SanitizeMailHTML(src string, dark bool) string {
	doc, _ := MailDocument(src, BodyThemeFor(velaTheme(dark)), MailHeader{})
	return doc
}
