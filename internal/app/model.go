// Package app 是 Vela 的原生界面（MyGo package ui，GPU 绘制）。
// 它替代了原来的 React 前端，直接调用 internal/core 的后端，不再经过 IPC。
// 只有 HTML 邮件正文放在内嵌的系统 WebView 里显示。
package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo/ui"
)

// Mailer 是界面用到的后端能力；生产环境是 *core.Backend，测试里是假实现。
type Mailer interface {
	Providers() []core.ProviderPreset
	GuessProvider(email string) string
	TestConnection(in core.AddAccountInput) ([]string, error)
	AddAccount(in core.AddAccountInput) (core.MailboxAccount, error)
	ListAccounts() ([]core.MailboxAccount, error)
	RemoveAccount(id string) error
	SyncAccount(id string) (core.SyncOutcome, error)
	SetFlags(account, remoteName string, uid uint32, read, starred *bool) error
	Archive(account, remoteName string, uid uint32) error
	Trash(account, remoteName string, uid uint32) error
	Send(in core.SendInput) (string, error)
	GetEmails() ([]core.Email, error)
	SyncEmails(e []core.Email) error
	DeleteEmails(ids []string) error
	AIChat(in core.AIChatInput) (string, error)
}

// Folder 是侧边栏的文件夹。
type Folder struct {
	ID, Label string
}

// Folders 与原前端 FOLDERS 相同。
var Folders = []Folder{
	{"inbox", "收件箱"}, {"starred", "星标"}, {"sent", "已发送"},
	{"drafts", "草稿"}, {"archive", "归档"}, {"trash", "垃圾箱"},
}

// Categories 是智能分类（原 SmartCategory）。
var Categories = []Folder{
	{"important", "重要"}, {"finance", "财务"}, {"travel", "出行"}, {"social", "社交"},
	{"newsletter", "订阅"}, {"notification", "通知"}, {"spam", "垃圾"},
}

// Settings 是持久化的偏好（原来放在 localStorage）。
type Settings struct {
	Theme         string `json:"theme"`   // system / light / dark
	Density       string `json:"density"` // cozy / compact
	SyncMinutes   int    `json:"syncMinutes"`
	Notifications bool   `json:"notifications"`
	ImportantOnly bool   `json:"importantOnly"`
	AIBaseURL     string `json:"aiBaseUrl"`
	AIKey         string `json:"aiKey"`
	AIModel       string `json:"aiModel"`
	AILanguage    string `json:"aiLanguage"`
}

// DefaultSettings 是首次运行的设置。
func DefaultSettings() Settings {
	return Settings{Theme: "system", Density: "cozy", SyncMinutes: 5, Notifications: true,
		AIBaseURL: "https://open.bigmodel.cn/api/paas/v4", AIModel: "glm-4-flash", AILanguage: "中文"}
}

// Compose 是正在写的邮件。
type Compose struct {
	Mode      string // new / reply / replyAll / forward
	AccountID string
	To, CC    string
	Subject   string
	Body      string
	InReplyTo string
	Sending   bool
}

// AddForm 是添加邮箱表单。
type AddForm struct {
	Provider string
	Email    string
	Password string
	Label    string
	IMAPHost string
	IMAPPort string
	SMTPHost string
	SMTPPort string
	Busy     bool
	Err      string
}

// App 是整个界面的状态。所有字段只在主线程（view 与 post 回调）里读写。
type App struct {
	be       Mailer
	accounts []core.MailboxAccount
	emails   []core.Email

	folder   string // 当前文件夹 / 分类
	account  string // "" = 全部账户
	selected string // 当前邮件 id
	query    string

	settings     Settings
	settingsPath string

	compose      *Compose
	composeOpen  bool
	add          AddForm
	addOpen      bool
	settingsOpen bool
	searchOpen   bool
	paletteQ     string

	syncing        bool
	syncedAt       time.Time
	aiLoading      string
	fresh          map[string]time.Time // 新到邮件 → 到达时间（短暂高亮）
	providerLabels []string

	listSel int
	toasts  []string

	// post 把后台结果送回主线程；async 为 false 时同步执行（测试用）。
	post  func(func())
	async bool
	// notify 弹系统通知（生产环境由 main 注入）。
	notify func(title, body string)
	// body 显示 HTML 正文（生产环境是 WebView）。
	body       BodyView
	bodyFailed map[string]bool // HTML 正文加载失败的邮件 → 改用纯文本
	now        func() time.Time
	dark       bool
}

// MarkBodyFailed 记录某封邮件的 HTML 正文在 WebView 中加载失败或超时，之后改用纯文本显示。
func (a *App) MarkBodyFailed(id string) {
	if a.bodyFailed == nil {
		a.bodyFailed = map[string]bool{}
	}
	a.bodyFailed[id] = true
}

// Dark 表示上一帧是否用了深色主题（WebView 正文跟随）。
func (a *App) Dark() bool { return a.dark }

// BodyView 是 HTML 正文的显示器。
type BodyView interface {
	// Show 加载一封邮件的 HTML 正文（doc 是 MailDocument 生成的完整文档）；
	// 同一 key 重复调用不会重新加载。返回 false 表示无法显示，界面改用纯文本。
	Show(key, doc string, bg string) bool
	// Native 返回要嵌进界面的 WebView。
	Native() ui.NativeWebView
}

// New 创建界面状态并从本地库恢复邮件。
func New(be Mailer, settingsPath string) *App {
	a := &App{be: be, folder: "inbox", settings: DefaultSettings(), settingsPath: settingsPath,
		fresh: map[string]time.Time{}, listSel: -1, post: func(f func()) { f() }, now: time.Now}
	a.loadSettings()
	a.accounts, _ = be.ListAccounts()
	a.emails, _ = be.GetEmails()
	for _, p := range be.Providers() {
		a.providerLabels = append(a.providerLabels, p.Label)
	}
	a.resetAdd()
	return a
}

// SetRuntime 注入主线程投递、通知和正文视图。
func (a *App) SetRuntime(post func(func()), notify func(title, body string), body BodyView) {
	a.post, a.async, a.notify, a.body = post, true, notify, body
}

func (a *App) loadSettings() {
	if a.settingsPath == "" {
		return
	}
	if b, err := os.ReadFile(a.settingsPath); err == nil {
		_ = json.Unmarshal(b, &a.settings)
	}
}

func (a *App) saveSettings() {
	if a.settingsPath == "" {
		return
	}
	b, _ := json.MarshalIndent(a.settings, "", "  ")
	_ = os.MkdirAll(filepath.Dir(a.settingsPath), 0o755)
	_ = os.WriteFile(a.settingsPath, b, 0o600)
}

// run 在后台执行 work，完成后在主线程执行 done。
func (a *App) run(work func() func()) {
	if !a.async {
		if done := work(); done != nil {
			done()
		}
		return
	}
	go func() {
		done := work()
		if done != nil {
			a.post(done)
		}
	}()
}

func (a *App) toast(msg string) { a.toasts = append(a.toasts, msg) }

// persist 把内存状态写回本地库（对应前端防抖保存 db_sync_emails）。
func (a *App) persist(changed ...core.Email) {
	if len(changed) == 0 {
		return
	}
	cp := slices.Clone(changed)
	a.run(func() func() {
		if err := a.be.SyncEmails(cp); err != nil {
			return func() { a.toast("保存到本地库失败：" + err.Error()) }
		}
		return nil
	})
}

/* ———— 查询 ———— */

func (a *App) find(id string) (int, *core.Email) {
	for i := range a.emails {
		if a.emails[i].ID == id {
			return i, &a.emails[i]
		}
	}
	return -1, nil
}

// Visible 返回当前文件夹 / 账户 / 搜索条件下的邮件，按时间倒序。
func (a *App) Visible() []*core.Email {
	q := strings.ToLower(strings.TrimSpace(a.query))
	var out []*core.Email
	for i := range a.emails {
		e := &a.emails[i]
		if a.account != "" && e.AccountID != a.account {
			continue
		}
		switch a.folder {
		case "starred":
			if !e.Starred || e.Folder == "trash" {
				continue
			}
		case "inbox", "sent", "drafts", "archive", "trash":
			if e.Folder != a.folder {
				continue
			}
		default: // 智能分类
			if e.Category != a.folder || e.Folder == "trash" {
				continue
			}
		}
		if q != "" && !matches(e, q) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out
}

func matches(e *core.Email, q string) bool {
	hay := strings.ToLower(e.Subject + "\n" + e.From.Name + "\n" + e.From.Email + "\n" + e.Body)
	return strings.Contains(hay, q)
}

// Unread 统计某文件夹未读数。
func (a *App) Unread(folder string) int {
	n := 0
	for _, e := range a.emails {
		if !e.Read && e.Folder == folder && (a.account == "" || e.AccountID == a.account) {
			n++
		}
	}
	return n
}

// Selected 返回当前邮件。
func (a *App) Selected() *core.Email {
	_, e := a.find(a.selected)
	return e
}

/* ———— 动作 ———— */

// SetFolder 切换文件夹。
func (a *App) SetFolder(id string) {
	a.folder, a.selected, a.listSel = id, "", -1
}

// Select 打开一封邮件并回写已读。
func (a *App) Select(id string) {
	_, e := a.find(id)
	if e == nil {
		return
	}
	a.selected = id
	if !e.Read {
		e.Read = true
		a.persist(*e)
		a.writeFlags(*e, ptr(true), nil, "已读状态")
	}
}

func ptr[T any](v T) *T { return &v }

func (a *App) writeFlags(e core.Email, read, starred *bool, what string) {
	if !e.Real {
		return
	}
	a.run(func() func() {
		if err := a.be.SetFlags(e.AccountID, e.RemoteName, e.UID, read, starred); err != nil {
			return func() { a.toast(what + "未能同步到服务器：" + err.Error()) }
		}
		return nil
	})
}

// ToggleStar 切换星标。
func (a *App) ToggleStar(id string) {
	_, e := a.find(id)
	if e == nil {
		return
	}
	e.Starred = !e.Starred
	a.persist(*e)
	a.writeFlags(*e, nil, ptr(e.Starred), "星标")
}

// ToggleRead 切换已读。
func (a *App) ToggleRead(id string) {
	_, e := a.find(id)
	if e == nil {
		return
	}
	e.Read = !e.Read
	a.persist(*e)
	a.writeFlags(*e, ptr(e.Read), nil, "已读状态")
}

// Move 把邮件移到 archive / trash（真实邮件同时移动服务器上的副本）。
func (a *App) Move(id, folder string) {
	i, e := a.find(id)
	if e == nil {
		return
	}
	if folder == "trash" && e.Folder == "trash" { // 垃圾箱里再删 = 彻底删除本地副本
		a.emails = slices.Delete(a.emails, i, i+1)
		a.selected = ""
		a.run(func() func() { _ = a.be.DeleteEmails([]string{id}); return nil })
		a.toast("已彻底删除")
		return
	}
	prev := *e
	e.Folder = folder
	if a.selected == id {
		a.selected = ""
	}
	a.persist(*e)
	label := map[string]string{"archive": "归档", "trash": "垃圾箱", "inbox": "收件箱"}[folder]
	a.toast("已移到" + label)
	if prev.Real && (folder == "archive" || folder == "trash") {
		a.run(func() func() {
			var err error
			if folder == "archive" {
				err = a.be.Archive(prev.AccountID, prev.RemoteName, prev.UID)
			} else {
				err = a.be.Trash(prev.AccountID, prev.RemoteName, prev.UID)
			}
			if err != nil {
				return func() { a.toast("服务器上的邮件未能移动：" + err.Error()) }
			}
			return nil
		})
	}
}

// Sync 同步全部账户：合并新邮件、剔除服务器上已不存在的副本、通知新到邮件。
func (a *App) Sync() {
	if a.syncing || len(a.accounts) == 0 {
		return
	}
	a.syncing = true
	accs := slices.Clone(a.accounts)
	a.run(func() func() {
		var outs []core.SyncOutcome
		for _, acc := range accs {
			o, err := a.be.SyncAccount(acc.ID)
			if err != nil {
				o = core.SyncOutcome{AccountID: acc.ID, Err: err}
			}
			outs = append(outs, o)
		}
		return func() { a.applySync(outs) }
	})
}

func (a *App) applySync(outs []core.SyncOutcome) {
	a.syncing = false
	a.syncedAt = a.now()
	total := 0
	more := false
	var arrivals []core.Email
	for _, o := range outs {
		if o.Err != nil {
			a.toast(fmt.Sprintf("%s 同步失败：%v", o.AccountID, o.Err))
			continue
		}
		added := a.merge(o.Emails)
		total += len(added)
		for _, e := range added {
			if e.IsNewArrival {
				arrivals = append(arrivals, e)
			}
		}
		a.prune(o.AccountID, o.Present)
		more = more || o.HasMoreHistory
	}
	switch {
	case total > 0:
		a.toast(fmt.Sprintf("已收取 %d 封邮件", total))
	default:
		a.toast("已是最新")
	}
	if more {
		a.toast("还有更早的邮件，再同步一次可继续加载")
	}
	a.notifyArrivals(arrivals)
}

// merge 按 id 去重合并，已存在的保留本地状态；返回真正新加入的邮件。
func (a *App) merge(in []core.Email) []core.Email {
	known := map[string]bool{}
	for _, e := range a.emails {
		known[e.ID] = true
	}
	var fresh []core.Email
	for _, e := range in {
		if known[e.ID] {
			continue
		}
		e.Category = Classify(&e)
		fresh = append(fresh, e)
		known[e.ID] = true
		if e.IsNewArrival {
			a.fresh[e.ID] = a.now()
		}
	}
	if len(fresh) > 0 {
		a.emails = append(fresh, a.emails...)
		a.persist(fresh...)
	}
	return fresh
}

// prune 移除「服务器上已删除 / 归档」的本地陈旧副本（只动 inbox / sent 里的真实邮件）。
func (a *App) prune(account string, present []core.FolderUIDs) {
	var gone []string
	keep := a.emails[:0]
	for _, e := range a.emails {
		drop := false
		if e.Real && e.AccountID == account {
			for _, p := range present {
				if p.Folder == e.Folder && !slices.Contains(p.UIDs, e.UID) {
					drop = true
				}
			}
		}
		if drop {
			gone = append(gone, e.ID)
		} else {
			keep = append(keep, e)
		}
	}
	a.emails = keep
	if len(gone) > 0 {
		a.run(func() func() { _ = a.be.DeleteEmails(gone); return nil })
	}
}

// OnNewMail 是后台 IDLE 监控推来的新邮件（原 vela://new-mail 事件）。
func (a *App) OnNewMail(account string, emails []core.Email) {
	added := a.merge(emails)
	if len(added) == 0 {
		return
	}
	a.syncedAt = a.now()
	a.toast(fmt.Sprintf("已收取 %d 封新邮件", len(added)))
	a.notifyArrivals(added)
}

func (a *App) notifyArrivals(arr []core.Email) {
	if len(arr) == 0 || a.notify == nil || !a.settings.Notifications {
		return
	}
	if a.settings.ImportantOnly {
		arr = slices.DeleteFunc(slices.Clone(arr), func(e core.Email) bool { return e.Category != "important" })
		if len(arr) == 0 {
			return
		}
	}
	brief := func(e core.Email) string {
		s := e.Subject
		if s == "" {
			s = "（无主题）"
		}
		return e.From.Name + "：" + s
	}
	if len(arr) == 1 {
		a.notify("Vela 轻帆 · 新邮件", brief(arr[0]))
		return
	}
	var lines []string
	for _, e := range arr[:min(3, len(arr))] {
		lines = append(lines, brief(e))
	}
	if len(arr) > 3 {
		lines = append(lines, fmt.Sprintf("…还有 %d 封", len(arr)-3))
	}
	a.notify(fmt.Sprintf("Vela 轻帆 · 收到 %d 封新邮件", len(arr)), strings.Join(lines, "\n"))
}

/* ———— 写信 ———— */

// StartCompose 打开写信窗口；mode 为 reply / replyAll / forward 时引用当前邮件。
func (a *App) StartCompose(mode string) {
	c := &Compose{Mode: mode}
	if len(a.accounts) > 0 {
		c.AccountID = a.accounts[0].ID
		if a.account != "" {
			c.AccountID = a.account
		}
	}
	if e := a.Selected(); e != nil && mode != "new" {
		if e.Real {
			c.AccountID = e.AccountID
		}
		quoted := fmt.Sprintf("\n\n——— 原始邮件 ———\n发件人：%s <%s>\n时间：%s\n主题：%s\n\n%s", e.From.Name, e.From.Email, e.Date, e.Subject, e.Body)
		switch mode {
		case "reply", "replyAll":
			c.To = e.From.Email
			c.Subject = prefixed("Re: ", e.Subject)
			c.InReplyTo = e.MessageID
			if mode == "replyAll" {
				var cc []string
				for _, x := range append(slices.Clone(e.To), e.CC...) {
					if x.Email != c.AccountID && x.Email != e.From.Email {
						cc = append(cc, x.Email)
					}
				}
				c.CC = strings.Join(cc, ", ")
			}
		case "forward":
			c.Subject = prefixed("Fwd: ", e.Subject)
		}
		c.Body = quoted
	}
	a.compose, a.composeOpen = c, true
}

func prefixed(p, s string) string {
	if strings.HasPrefix(strings.ToLower(s), strings.ToLower(p)) {
		return s
	}
	return p + s
}

func splitAddrs(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '，' || r == '；' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SendCompose 通过 SMTP 发送，成功后在「已发送」里留一份本地副本。
func (a *App) SendCompose() {
	c := a.compose
	if c == nil || c.Sending {
		return
	}
	if c.AccountID == "" {
		a.toast("请先添加一个邮箱账户")
		return
	}
	to := splitAddrs(c.To)
	if len(to) == 0 {
		a.toast("请至少填写一个收件人")
		return
	}
	c.Sending = true
	in := core.SendInput{AccountID: c.AccountID, To: to, CC: splitAddrs(c.CC), InReplyTo: c.InReplyTo, Subject: c.Subject, Body: c.Body}
	a.run(func() func() {
		_, err := a.be.Send(in)
		return func() {
			c.Sending = false
			if err != nil {
				a.toast(err.Error())
				return
			}
			now := a.now()
			var tos []core.Contact
			for _, t := range to {
				tos = append(tos, core.Contact{Name: t, Email: t})
			}
			sent := core.Email{ID: fmt.Sprintf("local:sent:%d", now.UnixNano()), AccountID: c.AccountID, Folder: "sent",
				From: core.Contact{Name: c.AccountID, Email: c.AccountID}, To: tos, Subject: c.Subject, Body: c.Body,
				Date: now.Format(time.RFC3339), Read: true, ThreadCount: 1, Attachments: []core.Attachment{}}
			a.emails = append([]core.Email{sent}, a.emails...)
			a.persist(sent)
			a.compose, a.composeOpen = nil, false
			a.toast("邮件已发送")
		}
	})
}

// SaveDraft 把写了一半的邮件存进草稿箱。
func (a *App) SaveDraft() {
	c := a.compose
	if c == nil {
		return
	}
	now := a.now()
	d := core.Email{ID: fmt.Sprintf("local:drafts:%d", now.UnixNano()), AccountID: c.AccountID, Folder: "drafts",
		From: core.Contact{Name: c.AccountID, Email: c.AccountID}, Subject: c.Subject, Body: c.Body,
		Date: now.Format(time.RFC3339), Read: true, ThreadCount: 1, Attachments: []core.Attachment{}}
	for _, t := range splitAddrs(c.To) {
		d.To = append(d.To, core.Contact{Name: t, Email: t})
	}
	a.emails = append([]core.Email{d}, a.emails...)
	a.persist(d)
	a.compose, a.composeOpen = nil, false
	a.toast("已存为草稿")
}

/* ———— 添加邮箱 ———— */

func (a *App) resetAdd() {
	a.add = AddForm{Provider: "QQ 邮箱", IMAPPort: "993", SMTPPort: "465"}
}

func (a *App) providerID(label string) string {
	for _, p := range a.be.Providers() {
		if p.Label == label {
			return p.ID
		}
	}
	return "custom"
}

func (a *App) providerHint() string {
	id := a.providerID(a.add.Provider)
	for _, p := range a.be.Providers() {
		if p.ID == id {
			return p.Hint
		}
	}
	return ""
}

// GuessFromEmail 按邮箱域名自动选服务商。
func (a *App) GuessFromEmail() {
	id := a.be.GuessProvider(a.add.Email)
	if id == "custom" {
		return
	}
	for _, p := range a.be.Providers() {
		if p.ID == id {
			a.add.Provider = p.Label
		}
	}
}

func atoiOr(s string, d int) int {
	var n int
	if _, err := fmt.Sscan(strings.TrimSpace(s), &n); err != nil {
		return d
	}
	return n
}

// SubmitAdd 验证登录并保存账户，然后立即同步一次。
func (a *App) SubmitAdd() {
	if a.add.Busy {
		return
	}
	f := a.add
	in := core.AddAccountInput{Provider: a.providerID(f.Provider), Email: f.Email, Password: f.Password, Label: f.Label,
		IMAPHost: f.IMAPHost, IMAPPort: atoiOr(f.IMAPPort, 993), SMTPHost: f.SMTPHost, SMTPPort: atoiOr(f.SMTPPort, 465)}
	a.add.Busy, a.add.Err = true, ""
	a.run(func() func() {
		acc, err := a.be.AddAccount(in)
		return func() {
			a.add.Busy = false
			if err != nil {
				a.add.Err = err.Error()
				return
			}
			a.accounts = slices.DeleteFunc(a.accounts, func(x core.MailboxAccount) bool { return x.ID == acc.ID })
			a.accounts = append(a.accounts, acc)
			a.addOpen = false
			a.resetAdd()
			a.toast("已接入 " + acc.Email + "，正在同步…")
			a.Sync()
		}
	})
}

// RemoveAccount 移除账户及其本地邮件。
func (a *App) RemoveAccount(id string) {
	a.run(func() func() {
		err := a.be.RemoveAccount(id)
		return func() {
			if err != nil {
				a.toast(err.Error())
				return
			}
			a.accounts = slices.DeleteFunc(a.accounts, func(x core.MailboxAccount) bool { return x.ID == id })
			a.emails = slices.DeleteFunc(a.emails, func(e core.Email) bool { return e.AccountID == id })
			if a.account == id {
				a.account = ""
			}
			a.selected = ""
			a.toast("已移除 " + id)
		}
	})
}

/* ———— AI ———— */

// AIReady 表示 AI 设置完整。
func (a *App) AIReady() bool {
	s := a.settings
	return strings.TrimSpace(s.AIBaseURL) != "" && strings.TrimSpace(s.AIKey) != "" && strings.TrimSpace(s.AIModel) != ""
}

// Analyze 用 AI（未配置时用本地规则）给邮件分类并生成摘要。
func (a *App) Analyze(id string) {
	_, e := a.find(id)
	if e == nil || a.aiLoading != "" {
		return
	}
	if !a.AIReady() {
		r := AnalyzeRules(e)
		e.Category, e.AISummary, e.AIWhy = r.Category, r.Summary, r.Why
		a.persist(*e)
		a.toast("已用本地规则分析（在设置里配置 AI 可获得更好的结果）")
		return
	}
	a.aiLoading = id
	in := core.AIChatInput{BaseURL: a.settings.AIBaseURL, APIKey: a.settings.AIKey, Model: a.settings.AIModel, Prompt: renderPrompt(e, a.settings.AILanguage)}
	a.run(func() func() {
		text, err := a.be.AIChat(in)
		return func() {
			a.aiLoading = ""
			_, e := a.find(id)
			if e == nil {
				return
			}
			if err != nil {
				a.toast("AI 分析失败：" + err.Error())
				return
			}
			r := ParseAnalysis(text)
			if r.Category != "" {
				e.Category = r.Category
			}
			e.AISummary, e.AIWhy = r.Summary, r.Why
			a.persist(*e)
		}
	})
}

// TestAI 发一句极短的对话测试 AI 设置。
func (a *App) TestAI() {
	in := core.AIChatInput{BaseURL: a.settings.AIBaseURL, APIKey: a.settings.AIKey, Model: a.settings.AIModel, Prompt: "请只回复两个字：你好"}
	a.run(func() func() {
		text, err := a.be.AIChat(in)
		return func() {
			if err != nil {
				a.toast("AI 测试失败：" + err.Error())
			} else {
				a.toast("AI 可用：" + text)
			}
		}
	})
}
