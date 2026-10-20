package app

import (
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
// 与原版相同，远程图片默认会加载。

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

const csp = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'none'; img-src * data: cid:; style-src 'unsafe-inline' *; font-src * data:; form-action 'none'">`

// SanitizeMailHTML 返回可以安全交给 WebView 的完整文档。
func SanitizeMailHTML(html string, dark bool) string {
	clean := mailPolicy().Sanitize(html)
	scheme := "light"
	if dark {
		scheme = "light dark"
	}
	return "<!doctype html><html><head><meta charset=\"utf-8\">" + csp +
		"<meta name=\"color-scheme\" content=\"" + scheme + "\"><style>body{margin:16px;font-family:-apple-system,'PingFang SC','Microsoft YaHei',sans-serif;word-wrap:break-word}img{max-width:100%;height:auto}</style></head><body>" +
		clean + "</body></html>"
}
