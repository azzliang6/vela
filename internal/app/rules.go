package app

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/azzliang6/vela/internal/core"
)

// 本地规则分类，移植自原 src/services/classifyService.ts。
var (
	spamHints     = []string{"中奖", "抢购资格", "代开", "点击领取", "退订回td", "点数低至", "仅需支付", "名额有限"}
	spamDomains   = []string{".win", ".top", ".xyz", ".club", ".vip", "lucky-star", "vip-163"}
	financeWords  = []string{"账单", "还款", "信用卡", "发票", "报销", "续费", "付款", "工资", "到账", "扣款", "退款", "免息", "分期"}
	travelWords   = []string{"航班", "值机", "行程", "车次", "出票", "登机", "座位", "起飞", "到达", "酒店", "高铁", "火车"}
	socialWords   = []string{"爬山", "聚会", "聚餐", "夜爬", "周末", "回归礼包", "新赛季", "皮肤", "点赞"}
	newsWords     = []string{"newsletter", "周刊", "周报", "日报", "digest", "退订", "降价", "促销", "折扣", "购物车"}
	notifyWords   = []string{"通知", "提醒", "待审批", "审批", "部署", "扩容", "取件码", "派送", "已送达", "到期", "释放", "发放"}
	importantTime = []string{"本周", "明早", "明天", "截止", "周三前", "周五前", "今天", "尽快", "小时"}
	importantAct  = []string{"务必", "需要你", "给我", "指派", "请于", "请查收", "收下", "立即", "带上", "排期"}
)

func hits(text string, words []string) []string {
	var out []string
	for _, w := range words {
		if strings.Contains(text, w) {
			out = append(out, w)
		}
	}
	return out
}

// Classify 按关键词规则分类；没有命中返回 ""。
func Classify(e *core.Email) string {
	hay := strings.ToLower(e.Subject + "\n" + e.From.Email + "\n" + e.From.Name + "\n" + e.Body)
	domain := ""
	if i := strings.LastIndexByte(e.From.Email, '@'); i >= 0 {
		domain = strings.ToLower(e.From.Email[i+1:])
	}
	if len(hits(hay, spamHints)) > 0 {
		return "spam"
	}
	for _, d := range spamDomains {
		if strings.Contains(domain, d) {
			return "spam"
		}
	}
	switch {
	case len(hits(hay, financeWords)) > 0:
		return "finance"
	case len(hits(hay, travelWords)) > 0:
		return "travel"
	case len(hits(hay, importantTime))+len(hits(hay, importantAct)) >= 2:
		return "important"
	case len(hits(hay, newsWords)) > 0:
		return "newsletter"
	case len(hits(hay, socialWords)) > 0:
		return "social"
	case len(hits(hay, notifyWords)) > 0:
		return "notification"
	}
	return ""
}

// Analysis 是一次分析结果。
type Analysis struct{ Category, Summary, Why string }

var (
	skipRe  = regexp.MustCompile(`^(尊敬的|你好|您好|各位|亲爱的|祝|此致|——|·|【|编辑导读)`)
	keyRe   = regexp.MustCompile(`(¥|\d|定档|截止|需要|务必|请|已|成功|出票|账单|发布|安排|提醒|到|将|总计|金额)`)
	splitRe = regexp.MustCompile(`[\n。！？!?]+`)
	tagRe   = regexp.MustCompile(`^【[^】]*】`)
	fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")
)

// AnalyzeRules 是不依赖 AI 的抽取式摘要 + 分类。
func AnalyzeRules(e *core.Email) Analysis {
	best, bestScore := "", -1<<30
	var first string
	for _, s := range splitRe.Split(e.Body, -1) {
		s = strings.TrimSpace(s)
		n := utf8.RuneCountInString(s)
		if n < 8 || n > 90 {
			continue
		}
		if first == "" {
			first = s
		}
		score := 0
		if keyRe.MatchString(s) {
			score += 3
		}
		if regexp.MustCompile(`\d`).MatchString(s) {
			score++
		}
		if skipRe.MatchString(s) {
			score -= 5
		}
		if score > bestScore {
			best, bestScore = s, score
		}
	}
	if best == "" {
		best = first
	}
	if best == "" {
		best = e.Subject
	}
	best = strings.TrimSpace(tagRe.ReplaceAllString(best, ""))
	if r := []rune(best); len(r) > 64 {
		best = string(r[:63]) + "…"
	}
	r := Analysis{Category: Classify(e), Summary: best}
	if r.Category == "important" {
		hay := strings.ToLower(e.Subject + "\n" + e.Body)
		found := append(first2(hits(hay, importantTime)), first2(hits(hay, importantAct))...)
		if len(found) > 0 {
			r.Why = "命中信号：" + strings.Join(found, "、")
		}
	}
	return r
}

func first2(s []string) []string { return s[:min(2, len(s))] }

const promptTemplate = `你是一位高效的邮件助手，请分析下面这封邮件，输出分类、一句话摘要和重要性说明。

要求：
- 输出语言：{{language}}
- summary：概括这封邮件最重要的内容，不超过 80 字
- why：仅当 category 为 important 时给出一句话理由，其余情况填空字符串
- category 必须从以下固定值中选择一个：
  important（重要/需要行动） finance（账单/财务） travel（出行/行程）
  social（社交/生活） newsletter（订阅/推广） notification（系统通知） spam（垃圾邮件）

只输出一个 JSON 对象，不要包含任何其他文字，格式：
{"category":"important","summary":"…","why":"…"}

邮件内容：
发件人：{{from_name}} <{{from_email}}>
主题：{{subject}}
正文：
{{body}}`

func renderPrompt(e *core.Email, lang string) string {
	body := e.Body
	if r := []rune(body); len(r) > 6000 {
		body = string(r[:6000])
	}
	if lang == "" {
		lang = "简体中文"
	}
	return strings.NewReplacer("{{language}}", lang, "{{from_name}}", e.From.Name, "{{from_email}}", e.From.Email,
		"{{subject}}", e.Subject, "{{body}}", body).Replace(promptTemplate)
}

var validCategories = map[string]bool{"important": true, "finance": true, "travel": true, "social": true, "newsletter": true, "notification": true, "spam": true}

// ParseAnalysis 宽松解析模型输出（容忍 ```json 围栏和前后闲话）。
func ParseAnalysis(text string) Analysis {
	cand := text
	if m := fenceRe.FindStringSubmatch(text); m != nil {
		cand = m[1]
	}
	if s, e := strings.Index(cand, "{"), strings.LastIndex(cand, "}"); s >= 0 && e > s {
		var obj struct{ Category, Summary, Why any }
		if json.Unmarshal([]byte(cand[s:e+1]), &obj) == nil {
			r := Analysis{}
			if c, ok := obj.Category.(string); ok && validCategories[c] {
				r.Category = c
			}
			if s, ok := obj.Summary.(string); ok {
				r.Summary = strings.TrimSpace(s)
			}
			if w, ok := obj.Why.(string); ok {
				r.Why = strings.TrimSpace(w)
			}
			if r.Summary != "" || r.Category != "" {
				return r
			}
		}
	}
	r := []rune(text)
	return Analysis{Summary: string(r[:min(200, len(r))])}
}
