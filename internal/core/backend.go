package core

import (
	"log"
	"strings"
	"sync"
	"time"
)

// Backend 汇总原 Tauri command 的全部能力，界面直接调用它的方法。
// 每个方法一一对应 src-tauri 里同名的命令（见 README 的映射表）。
// 网络方法会阻塞，界面需在 goroutine 里调用。
// crypto/secret 由互斥锁保护：备份还原会换密钥，监控在后台 goroutine 持续读它。
type Backend struct {
	DB     *DB
	mu     sync.Mutex
	crypto *Crypto
	secret string
}

// Crypto 返回当前加密器（备份还原换密钥后立即生效）。
func (b *Backend) Crypto() *Crypto {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.crypto
}

// Secret 返回当前密钥材料（导出备份时放进备份文件）。
func (b *Backend) Secret() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.secret
}

func (b *Backend) setCrypto(secret string, c *Crypto) {
	b.mu.Lock()
	b.secret, b.crypto = secret, c
	b.mu.Unlock()
}

// OpenBackend 打开默认位置（与 Tauri 版相同）的数据库和密钥。
func OpenBackend() (*Backend, error) {
	db, err := OpenDB(DataDir() + "/vela.db")
	if err != nil {
		return nil, err
	}
	secret, ok := LoadSecret(ConfigDir())
	var c *Crypto
	if ok {
		c = NewCrypto(secret)
	} else {
		secret, err = GenerateSecretFile(ConfigDir())
		if err != nil {
			db.Close()
			return nil, err
		}
		c = NewCrypto(secret)
	}
	b := &Backend{DB: db}
	b.setCrypto(secret, c)
	return b, nil
}

// Providers ← mail_providers
func (b *Backend) Providers() []ProviderPreset { return AllPresets() }

// GuessProvider ← mail_guess_provider
func (b *Backend) GuessProvider(email string) string { return GuessProvider(email) }

// TestConnection ← mail_test_connection
func (b *Backend) TestConnection(in AddAccountInput) ([]string, error) {
	cfg, err := in.Config()
	if err != nil {
		return nil, err
	}
	return TestConnection(cfg)
}

// AddAccount ← mail_add_account：先验证能登录再加密保存。
func (b *Backend) AddAccount(in AddAccountInput) (MailboxAccount, error) {
	label := strings.TrimSpace(in.Label)
	if label == "" {
		label = strings.TrimSpace(in.Email)
	}
	cfg, err := in.Config()
	if err != nil {
		return MailboxAccount{}, err
	}
	if _, err := TestConnection(cfg); err != nil {
		return MailboxAccount{}, err
	}
	return b.DB.UpsertAccount(cfg, label, b.Crypto())
}

// ListAccounts ← mail_list_accounts
func (b *Backend) ListAccounts() ([]MailboxAccount, error) { return b.DB.ListAccounts() }

// RemoveAccount ← mail_remove_account（同时清掉该账户的本地邮件，原版由前端清）
func (b *Backend) RemoveAccount(id string) error {
	if err := b.DB.DeleteAccount(id); err != nil {
		return err
	}
	return b.DB.DeleteAccountEmails(id)
}

// SyncAccount ← mail_sync_account
func (b *Backend) SyncAccount(id string) (SyncOutcome, error) {
	cfg, err := b.DB.LoadConfig(id, b.Crypto())
	if err != nil {
		return SyncOutcome{}, err
	}
	cursors, err := b.DB.LoadCursors(id)
	if err != nil {
		return SyncOutcome{}, err
	}
	res, err := SyncAccount(cfg, cursors)
	if err != nil {
		return SyncOutcome{}, err
	}
	if err := b.DB.SaveCursors(id, res.Cursors); err != nil {
		log.Printf("[vela] 保存同步游标失败：%v", err)
	}
	out := SyncOutcome{AccountID: id, Emails: res.Mails, Skipped: res.Skipped, Present: res.Present}
	for _, c := range res.Cursors {
		if c.HasPendingHistory() {
			out.HasMoreHistory = true
		}
	}
	return out, nil
}

// SyncAll ← mail_sync_all：单个账户失败不影响其它账户。
func (b *Backend) SyncAll() ([]SyncOutcome, error) {
	accs, err := b.DB.ListAccounts()
	if err != nil {
		return nil, err
	}
	var out []SyncOutcome
	for _, a := range accs {
		r, err := b.SyncAccount(a.ID)
		if err != nil {
			log.Printf("[vela] 账户 %s 同步失败：%v", a.ID, err)
			r = SyncOutcome{AccountID: a.ID, Err: err}
		}
		out = append(out, r)
	}
	return out, nil
}

// SetFlags ← mail_set_flags
func (b *Backend) SetFlags(account, remoteName string, uid uint32, read, starred *bool) error {
	cfg, err := b.DB.LoadConfig(account, b.Crypto())
	if err != nil {
		return err
	}
	return SetFlags(cfg, remoteName, uid, read, starred)
}

// Archive ← mail_archive
func (b *Backend) Archive(account, remoteName string, uid uint32) error {
	return b.move(account, remoteName, uid, FolderArchive)
}

// Trash ← mail_trash
func (b *Backend) Trash(account, remoteName string, uid uint32) error {
	return b.move(account, remoteName, uid, FolderTrash)
}

func (b *Backend) move(account, remoteName string, uid uint32, k FolderKind) error {
	cfg, err := b.DB.LoadConfig(account, b.Crypto())
	if err != nil {
		return err
	}
	return MoveMessage(cfg, remoteName, uid, k)
}

// SendInput ← mail_send 的参数
type SendInput struct {
	AccountID string
	To, CC    []string
	InReplyTo string
	Subject   string
	Body      string
}

// Send ← mail_send
func (b *Backend) Send(in SendInput) (string, error) {
	cfg, err := b.DB.LoadConfig(in.AccountID, b.Crypto())
	if err != nil {
		return "", err
	}
	return Send(cfg, OutgoingMail{To: in.To, CC: in.CC, InReplyTo: in.InReplyTo, Subject: in.Subject, Body: in.Body})
}

// GetEmails ← db_get_emails
func (b *Backend) GetEmails() ([]Email, error) { return b.DB.GetEmails() }

// SyncEmails ← db_sync_emails
func (b *Backend) SyncEmails(e []Email) error { return b.DB.UpsertEmails(e) }

// DeleteEmails ← db_delete_emails
func (b *Backend) DeleteEmails(ids []string) error { return b.DB.DeleteEmails(ids) }

// AIChat ← ai_chat
func (b *Backend) AIChat(in AIChatInput) (string, error) { return AIChat(in) }

/* ———— 新邮件实时监控（对应 Rust mail/watcher.rs）———— */

const (
	idleWait     = 10 * time.Minute
	rescanEvery  = 45 * time.Second
	backoffStart = 15 * time.Second
	backoffMax   = 5 * time.Minute
)

// Watcher 为每个账户维持一条 IDLE 长连接，新邮件到达时回调 onNew。
type Watcher struct {
	b       *Backend
	onNew   func(account string, emails []Email)
	mu      sync.Mutex
	running map[string]chan struct{}
	stop    chan struct{}
}

// StartWatcher 启动后台监控；每 45 秒重新扫描账户列表。
func (b *Backend) StartWatcher(onNew func(account string, emails []Email)) *Watcher {
	w := &Watcher{b: b, onNew: onNew, running: map[string]chan struct{}{}, stop: make(chan struct{})}
	go func() {
		t := time.NewTicker(rescanEvery)
		defer t.Stop()
		for {
			w.rescan()
			select {
			case <-t.C:
			case <-w.stop:
				return
			}
		}
	}()
	return w
}

// Stop 停止所有监控。
func (w *Watcher) Stop() {
	close(w.stop)
	w.mu.Lock()
	for id, ch := range w.running {
		close(ch)
		delete(w.running, id)
	}
	w.mu.Unlock()
}

func (w *Watcher) rescan() {
	accs, _ := w.b.DB.ListAccounts()
	live := map[string]bool{}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, a := range accs {
		live[a.ID] = true
		if _, ok := w.running[a.ID]; !ok {
			ch := make(chan struct{})
			w.running[a.ID] = ch
			go w.watch(a.ID, ch)
		}
	}
	for id, ch := range w.running {
		if !live[id] {
			close(ch)
			delete(w.running, id)
		}
	}
}

func (w *Watcher) watch(id string, stop chan struct{}) {
	backoff := backoffStart
	for {
		select {
		case <-stop:
			return
		default:
		}
		cfg, err := w.b.DB.LoadConfig(id, w.b.Crypto())
		if err != nil {
			log.Printf("[vela] 实时监控退出：账户 %s 已不可用", id)
			return
		}
		before, _ := w.b.DB.LoadCursors(id)
		res, err := IdleFetchNew(cfg, before, idleWait, stop)
		if err != nil {
			log.Printf("[vela] %s 实时监控断开：%v，%v 后重连", id, err, backoff)
			select {
			case <-time.After(backoff):
			case <-stop:
				return
			}
			backoff = min(backoff*2, backoffMax)
			continue
		}
		backoff = backoffStart
		if res == nil {
			continue
		}
		w.saveMerged(id, res.Cursors)
		var fresh []Email
		for _, m := range res.Mails {
			if m.IsNewArrival {
				fresh = append(fresh, m)
			}
		}
		if len(fresh) > 0 && w.onNew != nil {
			w.onNew(id, fresh)
		}
	}
}

// saveMerged 合并游标：只前进不后退（前台同步可能已经推进得更远）。
func (w *Watcher) saveMerged(id string, fetched []MailboxCursor) {
	merged, _ := w.b.DB.LoadCursors(id)
	for _, c := range fetched {
		if m := findCursor(merged, c.Folder); m != nil {
			if c.LastUID >= m.LastUID {
				*m = c
			}
		} else {
			merged = append(merged, c)
		}
	}
	if err := w.b.DB.SaveCursors(id, merged); err != nil {
		log.Printf("[vela] 保存实时同步游标失败：%v", err)
	}
}
