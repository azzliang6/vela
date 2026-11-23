package app

import (
	"fmt"
	"hash/fnv"
	"html"
	"regexp"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
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

// BodyTheme 是正文文档的配色，全部取自界面实际使用的主题（velaTheme / velaPalette），
// 不另外硬编码：Bg 就是阅读区的底色 Theme.Background。
type BodyTheme struct {
	Dark                                    bool
	Bg, Text, Muted, Accent, Border, Select string
	CardBg, SubtleBorder, AccentSoft        string
	AccentBorder, TextSoft                  string
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
		w = 8
	}
	p := velaPalette(t.Dark)
	return BodyTheme{
		Dark: t.Dark, Bg: cssColor(t.Background), Text: cssColor(t.Text), Muted: cssColor(t.TextMuted),
		Accent: cssColor(t.Accent), Border: cssColor(t.Border), Select: cssColor(t.Selection),
		CardBg: cssColor(p.CardBg), SubtleBorder: cssColor(p.SubtleDivider),
		AccentSoft: cssColor(p.AccentSoft), AccentBorder: cssColor(p.AccentBorder), TextSoft: cssColor(p.TextSoft),
		Scrollbar: cssColor(t.Scrollbar), ScrollbarWidth: w,
	}
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

func headerHTML(h MailHeader, dark bool, remote bool) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<header class="vela-h"><h1>` + esc(h.Subject) + `</h1><div class="vela-from">`)
	seed := nonEmpty(h.FromName, h.FromEmail)
	avBg, avFg := avatarTone(seed, dark)
	initial := senderInitial(h.FromName, h.FromEmail)
	b.WriteString(`<span class="vela-av" style="background:` + cssColor(avBg) + `;color:` + cssColor(avFg) + `">` + esc(initial) + `</span>`)
	b.WriteString(`<div class="vela-who"><div class="vela-sender"><b>` + esc(nonEmpty(h.FromName, h.FromEmail)) + `</b>`)
	if h.FromEmail != "" && h.FromName != "" {
		b.WriteString(` <span class="vela-addr">&lt;` + esc(h.FromEmail) + `&gt;</span>`)
	}
	b.WriteString(`</div>`)
	if h.Recipients != "" {
		b.WriteString(`<div class="vela-m">` + esc(h.Recipients) + `</div>`)
	}
	b.WriteString(`</div>`)
	if h.Date != "" {
		b.WriteString(`<span class="vela-m vela-date">` + esc(h.Date) + `</span>`)
	}
	b.WriteString(`</div>`)
	if h.Summary != "" {
		b.WriteString(`<div class="vela-ai"><div class="vela-ai-t">✦ ` + esc(h.SummaryTitle) + `</div><div class="vela-ai-s">` + esc(h.Summary) + `</div>`)
		if h.Why != "" {
			b.WriteString(`<div class="vela-m vela-ai-w">` + esc(h.Why) + `</div>`)
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
		b.WriteString(`<div class="vela-blocked">已阻止远程图片以保护隐私</div>`)
	}
	b.WriteString(`</header>`)
	return b.String()
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
	accentSoft := nonEmpty(th.AccentSoft, th.Select)
	accentBorder := nonEmpty(th.AccentBorder, th.Border)
	cardBg := nonEmpty(th.CardBg, th.Bg)
	subtleBorder := nonEmpty(th.SubtleBorder, th.Border)
	css := "html,body{margin:0!important;padding:0!important;border:0!important;background:" + th.Bg + "!important;color:" + th.Text + "}" +
		"html{--scrollbar-thumb:" + th.Scrollbar + ";--scrollbar-track:transparent;accent-color:" + th.Accent + ";scrollbar-width:auto;scrollbar-color:" + th.Scrollbar + " transparent}" +
		"::selection{background:" + th.Select + "}" +
		"@supports not (scrollbar-color: auto){" +
		"::-webkit-scrollbar{width:" + sw + ";height:" + sw + ";background:var(--scrollbar-track)}" +
		"::-webkit-scrollbar-track,::-webkit-scrollbar-corner{background:var(--scrollbar-track)}" +
		"::-webkit-scrollbar-thumb{background:var(--scrollbar-thumb);border-radius:" + sw + "}" +
		"::-webkit-scrollbar-button{display:none}" +
		"}" +
		".vela-wrap{font:14.5px/1.72 -apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Noto Sans CJK SC','Microsoft YaHei',sans-serif;word-wrap:break-word;overflow-wrap:anywhere}" +
		".vela-h{max-width:820px;padding:28px 36px 20px;border-bottom:1px solid " + subtleBorder + ";font:14px/1.5 -apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Noto Sans CJK SC','Microsoft YaHei',sans-serif;color:" + th.Text + "}" +
		".vela-h h1{font-size:23px;line-height:1.35;letter-spacing:-0.01em;margin:0 0 14px;font-weight:700}" +
		".vela-from{display:flex;align-items:center;gap:12px}.vela-who{flex:1;min-width:0}" +
		".vela-sender{font-size:14px;line-height:1.4}.vela-addr{color:" + th.Muted + ";font-weight:400;font-size:13px}" +
		".vela-av{flex:none;width:38px;height:38px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:15px;font-weight:600}" +
		".vela-m{font-size:12px;color:" + th.Muted + ";margin-top:2px}.vela-date{flex:none;margin-top:0}" +
		".vela-ai{margin-top:14px;padding:12px 16px;border-radius:11px;background:" + accentSoft + ";border:1px solid " + accentBorder + "}" +
		".vela-ai-t{font-size:12px;font-weight:700;letter-spacing:0.02em;color:" + th.Accent + ";margin-bottom:4px}" +
		".vela-ai-s{font-size:13.5px;line-height:1.55}.vela-ai-w{margin-top:4px}" +
		".vela-att{margin-top:12px;display:flex;flex-wrap:wrap;gap:8px}.vela-att span{font-size:12.5px;padding:6px 12px;background:" + cardBg + ";border:1px solid " + th.Border + ";border-radius:9px}" +
		".vela-blocked{display:inline-block;margin-top:10px;padding:3px 10px;border-radius:999px;font-size:11.5px;color:" + th.Muted + ";background:" + cardBg + ";border:1px solid " + th.Border + "}" +
		".vela-body{max-width:820px;padding:22px 36px 44px}.vela-body a{color:" + th.Accent + ";text-underline-offset:2px}.vela-body img{max-width:100%;height:auto;border-radius:6px}.vela-body table{max-width:100%}" +
		".vela-body blockquote{margin:8px 0 8px 2px;padding:2px 0 2px 14px;border-left:3px solid " + th.Border + ";color:" + th.Muted + "}"
	doc = "<!doctype html><html><head><meta charset=\"utf-8\">" +
		"<meta http-equiv=\"Content-Security-Policy\" content=\"" + MailCSP + "\">" +
		"<meta name=\"color-scheme\" content=\"" + scheme + "\">" +
		"<meta name=\"viewport\" content=\"width=device-width\">" +
		"<style>" + css + "</style></head><body><div class=\"vela-wrap\">" + headerHTML(h, th.Dark, remote) +
		"<div class=\"vela-body\">" + clean + "</div></div></body></html>"
	return doc, remote
}

// SanitizeMailHTML 保留旧接口：按主题生成正文文档。
func SanitizeMailHTML(src string, dark bool) string {
	doc, _ := MailDocument(src, BodyThemeFor(velaTheme(dark)), MailHeader{})
	return doc
}
