package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // 纯 Go SQLite，CGO_ENABLED=0 可用
)

// Schema 与 Rust localdb::SCHEMA 相同，另加几列真实邮件元数据（新列可空，
// 老库打开时自动 ALTER TABLE 补上，Rust 版写入的数据原样可读）。
const Schema = `
CREATE TABLE IF NOT EXISTS emails (
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL,
  folder       TEXT NOT NULL,
  sender       TEXT NOT NULL,
  recipients   TEXT NOT NULL,
  cc           TEXT,
  subject      TEXT NOT NULL,
  body         TEXT NOT NULL,
  html_body    TEXT,
  date         TEXT NOT NULL,
  read         INTEGER NOT NULL DEFAULT 0,
  starred      INTEGER NOT NULL DEFAULT 0,
  category     TEXT,
  ai_summary   TEXT,
  ai_why       TEXT,
  attachments  TEXT NOT NULL DEFAULT '[]',
  thread_count INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_emails_date ON emails(date);

CREATE TABLE IF NOT EXISTS mailbox_accounts (
  id            TEXT PRIMARY KEY,
  email         TEXT NOT NULL,
  provider      TEXT NOT NULL DEFAULT 'custom',
  label         TEXT NOT NULL DEFAULT '',
  imap_host     TEXT NOT NULL,
  imap_port     INTEGER NOT NULL,
  imap_security TEXT NOT NULL DEFAULT 'ssl',
  smtp_host     TEXT NOT NULL,
  smtp_port     INTEGER NOT NULL,
  smtp_security TEXT NOT NULL DEFAULT 'ssl',
  secret_enc    TEXT NOT NULL,
  cursors       TEXT NOT NULL DEFAULT '[]'
);
`

var extraColumns = []string{
	"ALTER TABLE emails ADD COLUMN uid INTEGER",
	"ALTER TABLE emails ADD COLUMN remote_name TEXT",
	"ALTER TABLE emails ADD COLUMN message_id TEXT",
}

// DB 是本地单文件数据库。SQLite 连接用互斥锁串行化。
type DB struct {
	mu sync.Mutex
	db *sql.DB
}

// OpenDB 打开（必要时创建）数据库并建表。path 为 ":memory:" 时用内存库。
func OpenDB(path string) (*DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("无法创建数据目录：%v", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("无法打开本地数据库：%v", err)
	}
	db.SetMaxOpenConns(1)
	if path != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			return nil, fmt.Errorf("设置 WAL 失败：%v", err)
		}
	}
	if _, err := db.Exec(Schema); err != nil {
		return nil, fmt.Errorf("初始化本地数据库失败：%v", err)
	}
	for _, q := range extraColumns {
		_, _ = db.Exec(q) // 列已存在时报错，忽略
	}
	return &DB{db: db}, nil
}

// Close 关闭数据库。
func (d *DB) Close() error { return d.db.Close() }

func stripNUL(s string) string { return strings.ReplaceAll(s, "\x00", "") }

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// GetEmails 读取全部邮件，按时间倒序（对应 db_get_emails）。
func (d *DB) GetEmails() ([]Email, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.db.Query(`SELECT id, account_id, folder, sender, recipients, cc, subject, body,
		html_body, date, read, starred, category, ai_summary, ai_why, attachments, thread_count,
		uid, remote_name, message_id FROM emails ORDER BY date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Email
	for rows.Next() {
		var e Email
		var sender, recipients, attachments string
		var cc, html, cat, sum, why, remote, mid sql.NullString
		var uid sql.NullInt64
		var read, starred int64
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Folder, &sender, &recipients, &cc, &e.Subject, &e.Body,
			&html, &e.Date, &read, &starred, &cat, &sum, &why, &attachments, &e.ThreadCount,
			&uid, &remote, &mid); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(sender), &e.From)
		_ = json.Unmarshal([]byte(recipients), &e.To)
		if cc.Valid {
			_ = json.Unmarshal([]byte(cc.String), &e.CC)
		}
		_ = json.Unmarshal([]byte(attachments), &e.Attachments)
		e.HTMLBody, e.Category, e.AISummary, e.AIWhy = html.String, cat.String, sum.String, why.String
		e.Read, e.Starred = read != 0, starred != 0
		e.RemoteName, e.MessageID = remote.String, mid.String
		if uid.Valid {
			e.UID, e.Real = uint32(uid.Int64), true
		} else if acc, f, u, ok := ParseEmailID(e.ID); ok && acc == e.AccountID {
			// Rust 版没有存 uid：从稳定 id 还原，远端文件夹名留空（回写时按 INBOX / 已发送探测）
			e.UID, e.Real = u, true
			if f == "inbox" {
				e.RemoteName = "INBOX"
			}
		}
		if e.Real {
			if strings.Contains(e.ID, ":sent:") {
				e.ServerFolder = "sent"
			} else {
				e.ServerFolder = "inbox"
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpsertEmails 全量 upsert（对应 db_sync_emails）。
func (d *DB) UpsertEmails(emails []Email) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	if err := d.upsertEmailsTx(tx, emails); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// upsertEmailsTx 在事务里全量 upsert 邮件；调用方持有 d.mu。
func (d *DB) upsertEmailsTx(tx *sql.Tx, emails []Email) error {
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO emails
		(id, account_id, folder, sender, recipients, cc, subject, body, html_body,
		 date, read, starred, category, ai_summary, ai_why, attachments, thread_count,
		 uid, remote_name, message_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	js := func(v any) string { b, _ := json.Marshal(v); return stripNUL(string(b)) }
	for _, e := range emails {
		to := e.To
		if to == nil {
			to = []Contact{}
		}
		att := e.Attachments
		if att == nil {
			att = []Attachment{}
		}
		var cc any
		if len(e.CC) > 0 {
			cc = js(e.CC)
		}
		var uid any
		if e.Real {
			uid = int64(e.UID)
		}
		tc := e.ThreadCount
		if tc == 0 {
			tc = 1
		}
		b2i := func(b bool) int {
			if b {
				return 1
			}
			return 0
		}
		if _, err := stmt.Exec(stripNUL(e.ID), stripNUL(e.AccountID), e.Folder, js(e.From), js(to), cc,
			stripNUL(e.Subject), stripNUL(e.Body), nullStr(stripNUL(e.HTMLBody)), e.Date,
			b2i(e.Read), b2i(e.Starred), nullStr(e.Category), nullStr(stripNUL(e.AISummary)),
			nullStr(stripNUL(e.AIWhy)), js(att), tc, uid, nullStr(e.RemoteName), nullStr(e.MessageID)); err != nil {
			return fmt.Errorf("写入邮件失败：%v", err)
		}
	}
	return nil
}

// DeleteEmails 删除指定邮件（对应 db_delete_emails）。
func (d *DB) DeleteEmails(ids []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, id := range ids {
		if _, err := d.db.Exec("DELETE FROM emails WHERE id = ?", id); err != nil {
			return err
		}
	}
	return nil
}

// DeleteAccountEmails 删除某账户的全部邮件。
func (d *DB) DeleteAccountEmails(account string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec("DELETE FROM emails WHERE account_id = ?", account)
	return err
}

/* ———— 账户与游标（对应 Rust mail/store.rs）———— */

// UpsertAccount 新增或更新账户，授权码加密存储。
func (d *DB) UpsertAccount(cfg MailServerConfig, label string, c *Crypto) (MailboxAccount, error) {
	enc, err := c.Encrypt(cfg.Password)
	if err != nil {
		return MailboxAccount{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err = d.db.Exec(`INSERT INTO mailbox_accounts
		(id, email, provider, label, imap_host, imap_port, imap_security, smtp_host, smtp_port, smtp_security, secret_enc)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET provider=excluded.provider, label=excluded.label,
		imap_host=excluded.imap_host, imap_port=excluded.imap_port, imap_security=excluded.imap_security,
		smtp_host=excluded.smtp_host, smtp_port=excluded.smtp_port, smtp_security=excluded.smtp_security,
		secret_enc=excluded.secret_enc`,
		cfg.Email, cfg.Email, cfg.Provider, label, cfg.IMAPHost, cfg.IMAPPort, string(cfg.IMAPSecurity),
		cfg.SMTPHost, cfg.SMTPPort, string(cfg.SMTPSecurity), enc)
	if err != nil {
		return MailboxAccount{}, fmt.Errorf("保存邮箱账户失败：%v", err)
	}
	return MailboxAccount{ID: cfg.Email, Email: cfg.Email, Provider: cfg.Provider, Label: label,
		IMAPHost: cfg.IMAPHost, IMAPPort: cfg.IMAPPort, SMTPHost: cfg.SMTPHost, SMTPPort: cfg.SMTPPort}, nil
}

// ListAccounts 列出账户（不含凭证）。
func (d *DB) ListAccounts() ([]MailboxAccount, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.db.Query(`SELECT id, email, provider, label, imap_host, imap_port, smtp_host, smtp_port
		FROM mailbox_accounts ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("读取邮箱账户失败：%v", err)
	}
	defer rows.Close()
	var out []MailboxAccount
	for rows.Next() {
		var a MailboxAccount
		if err := rows.Scan(&a.ID, &a.Email, &a.Provider, &a.Label, &a.IMAPHost, &a.IMAPPort, &a.SMTPHost, &a.SMTPPort); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LoadConfig 取回含解密授权码的连接配置。
func (d *DB) LoadConfig(account string, c *Crypto) (MailServerConfig, error) {
	d.mu.Lock()
	var cfg MailServerConfig
	var isec, ssec, enc string
	err := d.db.QueryRow(`SELECT email, provider, imap_host, imap_port, imap_security,
		smtp_host, smtp_port, smtp_security, secret_enc FROM mailbox_accounts WHERE id = ?`, account).
		Scan(&cfg.Email, &cfg.Provider, &cfg.IMAPHost, &cfg.IMAPPort, &isec, &cfg.SMTPHost, &cfg.SMTPPort, &ssec, &enc)
	d.mu.Unlock()
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, errors.New("邮箱账户不存在")
	} else if err != nil {
		return cfg, fmt.Errorf("读取邮箱账户失败：%v", err)
	}
	pw, err := c.Decrypt(enc)
	if err != nil {
		return cfg, err
	}
	cfg.Password = pw
	cfg.IMAPSecurity, cfg.SMTPSecurity = secFrom(isec), secFrom(ssec)
	if cfg.Provider == "custom" {
		if strings.TrimSpace(cfg.IMAPHost) == "" || strings.TrimSpace(cfg.SMTPHost) == "" {
			return cfg, errors.New("该自定义账户缺少服务器地址，请重新添加")
		}
	} else {
		_ = ApplyPreset(&cfg)
	}
	return cfg, nil
}

func secFrom(s string) Security {
	switch s {
	case "starttls":
		return SecurityStartTLS
	case "none":
		return SecurityNone
	}
	return SecuritySSL
}

// DeleteAccount 删除账户。
func (d *DB) DeleteAccount(account string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.db.Exec("DELETE FROM mailbox_accounts WHERE id = ?", account); err != nil {
		return fmt.Errorf("删除邮箱账户失败：%v", err)
	}
	return nil
}

// LoadCursors 读取同步游标；损坏时按首次同步处理。
func (d *DB) LoadCursors(account string) ([]MailboxCursor, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var text string
	err := d.db.QueryRow("SELECT cursors FROM mailbox_accounts WHERE id = ?", account).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("读取同步游标失败：%v", err)
	}
	var cs []MailboxCursor
	if json.Unmarshal([]byte(text), &cs) != nil {
		return nil, nil
	}
	return cs, nil
}

// SaveCursors 写回同步游标。
func (d *DB) SaveCursors(account string, cs []MailboxCursor) error {
	if cs == nil {
		cs = []MailboxCursor{}
	}
	b, _ := json.Marshal(cs)
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.db.Exec("UPDATE mailbox_accounts SET cursors = ? WHERE id = ?", string(b), account); err != nil {
		return fmt.Errorf("保存同步游标失败：%v", err)
	}
	return nil
}
