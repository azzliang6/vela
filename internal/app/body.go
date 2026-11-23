package app

import (
	"slices"

	"github.com/azzliang6/vela/internal/core"
)

// HTML 正文按需加载：列表里的邮件不带 HTML（core.Email.HasHTML 标记库里有），
// 打开时在后台读取并放进一个小缓存；刚同步下来的新邮件直接把 HTML 放进缓存，
// 免得在写入本地库之前就被打开时读不到。

// htmlCacheCap 是内存里最多保留的 HTML 正文封数（最近打开 / 刚收到的）。
const htmlCacheCap = 24

type htmlCache struct {
	m       map[string]string
	order   []string        // 从旧到新
	loading map[string]bool // 正在后台读取的邮件
}

func (h *htmlCache) get(id string) (string, bool) {
	s, ok := h.m[id]
	return s, ok
}

func (h *htmlCache) put(id, html string) {
	if h.m == nil {
		h.m = map[string]string{}
	}
	if _, ok := h.m[id]; ok {
		h.order = slices.DeleteFunc(h.order, func(x string) bool { return x == id })
	}
	h.m[id] = html
	h.order = append(h.order, id)
	for len(h.order) > htmlCacheCap {
		delete(h.m, h.order[0])
		h.order = h.order[1:]
	}
}

func (h *htmlCache) reset() { *h = htmlCache{} }

// stripHTML 把邮件的 HTML 正文移进缓存，内存里的副本只留 HasHTML 标记。
// 调用前 HTML 应已（或正在）写入本地库。
func (a *App) stripHTML(e *core.Email) {
	if e.HTMLBody == "" {
		return
	}
	a.html.put(e.ID, e.HTMLBody)
	e.HasHTML, e.HTMLBody = true, ""
}

// htmlFor 返回邮件的 HTML 正文。ready 为 false 表示正在从本地库读取（读完会刷新界面）。
func (a *App) htmlFor(e *core.Email) (html string, ready bool) {
	if e.HTMLBody != "" || !e.HasHTML {
		return e.HTMLBody, true
	}
	if s, ok := a.html.get(e.ID); ok {
		return s, true
	}
	if a.html.loading[e.ID] {
		return "", false
	}
	if a.html.loading == nil {
		a.html.loading = map[string]bool{}
	}
	id, gen := e.ID, a.dataGen
	a.html.loading[id] = true
	a.run(func() func() {
		s, err := a.be.EmailHTML(id)
		return func() {
			delete(a.html.loading, id)
			if gen != a.dataGen {
				return // 期间还原了备份：同一 id 可能已是另一封邮件
			}
			if err != nil {
				a.MarkBodyFailed(id) // 读不出来就用纯文本，不反复重试
				return
			}
			a.html.put(id, s)
		}
	})
	if s, ok := a.html.get(id); ok { // 同步执行（测试）时已经读完
		return s, true
	}
	return "", false
}

// docMemo 记住上一次生成的正文文档：清洗上百 KB 的 HTML、算文档哈希都很贵，
// 每帧都会走到这里，只有邮件、主题或抬头变了才重新生成。
type docMemo struct {
	id, src string
	th      BodyTheme
	h       MailHeader
	doc     string
	key     string
}

func (a *App) mailDocument(id, src string, th BodyTheme, h MailHeader) (doc, key string) {
	m := &a.docMemo
	if m.doc != "" && m.id == id && m.src == src && m.th == th && sameHeader(m.h, h) {
		return m.doc, m.key
	}
	doc, _ = MailDocument(src, th, h)
	*m = docMemo{id: id, src: src, th: th, h: h, doc: doc, key: docKey(doc)}
	return m.doc, m.key
}

func sameHeader(x, y MailHeader) bool {
	return x.Subject == y.Subject && x.FromName == y.FromName && x.FromEmail == y.FromEmail &&
		x.Recipients == y.Recipients && x.Date == y.Date && x.SummaryTitle == y.SummaryTitle &&
		x.Summary == y.Summary && x.Why == y.Why && slices.Equal(x.Attachments, y.Attachments)
}
