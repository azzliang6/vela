package app

import (
	"regexp"
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

// BodyTheme 是正文文档的配色，与 theme.go 的阅读区一致。
type BodyTheme struct {
	Dark                    bool
	Bg, Text, Muted, Accent string
}

// BodyThemeFor 返回与原生界面一致的正文配色：背景等于阅读区（窗口背景）。
func BodyThemeFor(dark bool) BodyTheme {
	if dark {
		return BodyTheme{Dark: true, Bg: "#141210", Text: "#eae3d4", Muted: "#8c8272", Accent: "#f2611d"}
	}
	return BodyTheme{Bg: "#f7f4ea", Text: "#1d1a15", Muted: "#867d6d", Accent: "#d9480f"}
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

// MailDocument 把邮件 HTML 清洗后包成完整文档，并注入跟随应用主题的基础样式。
// remote 表示原文引用了被阻止的远程图片。
func MailDocument(html string, th BodyTheme) (doc string, remote bool) {
	clean := mailPolicy().Sanitize(html)
	remote = remoteImg.MatchString(clean)
	scheme := "light"
	if th.Dark {
		scheme = "dark"
	}
	css := "html,body{margin:0;background:" + th.Bg + ";color:" + th.Text + "}" +
		"body{padding:4px 28px 28px;font:14px/1.6 -apple-system,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;word-wrap:break-word;overflow-wrap:anywhere}" +
		"a{color:" + th.Accent + "}img{max-width:100%;height:auto}table{max-width:100%}" +
		"blockquote{margin:0 0 0 4px;padding-left:12px;border-left:3px solid " + th.Muted + ";color:" + th.Muted + "}" +
		"::-webkit-scrollbar{width:10px;height:10px}::-webkit-scrollbar-thumb{background:" + th.Muted + "55;border-radius:5px}"
	doc = "<!doctype html><html><head><meta charset=\"utf-8\">" +
		"<meta http-equiv=\"Content-Security-Policy\" content=\"" + MailCSP + "\">" +
		"<meta name=\"color-scheme\" content=\"" + scheme + "\">" +
		"<meta name=\"viewport\" content=\"width=device-width\">" +
		"<style>" + css + "</style></head><body>" + clean + "</body></html>"
	return doc, remote
}

// SanitizeMailHTML 保留旧接口：按主题生成正文文档。
func SanitizeMailHTML(html string, dark bool) string {
	doc, _ := MailDocument(html, BodyThemeFor(dark))
	return doc
}
