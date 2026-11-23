package core

import (
	"fmt"
	"strconv"
	"strings"
)

// Contact 是收发件人。
type Contact struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Attachment 是附件元信息（只保留名称、大小、类型，不下载内容）。
type Attachment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	SizeKB int    `json:"sizeKb"`
	// Kind: pdf / sheet / doc / image / zip / code
	Kind string `json:"kind"`
}

// Email 是界面和本地库共用的一封邮件，字段与原前端 types.ts 的 Email 对齐。
type Email struct {
	ID          string       `json:"id"`
	AccountID   string       `json:"accountId"`
	Folder      string       `json:"folder"` // inbox / sent / drafts / archive / trash
	From        Contact      `json:"from"`
	To          []Contact    `json:"to"`
	CC          []Contact    `json:"cc,omitempty"`
	Subject     string       `json:"subject"`
	Body        string       `json:"body"`
	HTMLBody    string       `json:"htmlBody,omitempty"`
	Date        string       `json:"date"` // RFC3339
	Read        bool         `json:"read"`
	Starred     bool         `json:"starred"`
	Category    string       `json:"category,omitempty"`
	AISummary   string       `json:"aiSummary,omitempty"`
	AIWhy       string       `json:"aiWhy,omitempty"`
	Attachments []Attachment `json:"attachments"`
	ThreadCount int          `json:"threadCount"`

	// 以下仅真实邮箱邮件有值，用于把状态回写到服务器。
	UID          uint32 `json:"uid,omitempty"`
	RemoteName   string `json:"remoteName,omitempty"`
	ServerFolder string `json:"serverFolder,omitempty"`
	MessageID    string `json:"messageId,omitempty"`
	IsNewArrival bool   `json:"-"`
	Real         bool   `json:"real,omitempty"`

	// HasHTML 表示本地库里存有 HTML 正文。界面列表不把 HTML 读进内存（ListEmails），
	// 打开邮件时再按 id 读取（EmailHTML）；此时 HTMLBody 为空而 HasHTML 为 true。
	HasHTML bool `json:"-"`
}

// EmailID 返回真实邮件稳定的 id：账户:文件夹:UID。
func EmailID(account, folder string, uid uint32) string {
	return fmt.Sprintf("%s:%s:%d", account, folder, uid)
}

// ParseEmailID 从 id 还原账户、文件夹和 UID（兼容 Rust 版库里没有 uid 列的行）。
func ParseEmailID(id string) (account, folder string, uid uint32, ok bool) {
	i := strings.LastIndexByte(id, ':')
	if i < 0 {
		return
	}
	n, err := strconv.ParseUint(id[i+1:], 10, 32)
	if err != nil {
		return
	}
	rest := id[:i]
	j := strings.LastIndexByte(rest, ':')
	if j < 0 {
		return
	}
	return rest[:j], rest[j+1:], uint32(n), true
}

// MailboxCursor 是增量同步游标，JSON 形状与 Rust 版一致（存在 mailbox_accounts.cursors）。
type MailboxCursor struct {
	Folder  string `json:"folder"` // inbox / sent
	LastUID uint32 `json:"lastUid"`
	// BackfillUID：nil = 尚未开始回填，0 = 已完成，n = 还需拉取 uid <= n
	BackfillUID *uint32 `json:"backfillUid"`
	UIDValidity uint32  `json:"uidValidity"`
	RemoteName  string  `json:"remoteName"`
}

// HasPendingHistory 表示还有更早的历史邮件待拉取。
func (c MailboxCursor) HasPendingHistory() bool { return c.BackfillUID != nil && *c.BackfillUID > 0 }

// MailboxAccount 是不含凭证的账户信息。
type MailboxAccount struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	IMAPHost string `json:"imapHost"`
	IMAPPort int    `json:"imapPort"`
	SMTPHost string `json:"smtpHost"`
	SMTPPort int    `json:"smtpPort"`
}

// FolderUIDs 是某文件夹当前在服务器上的全部 UID。
type FolderUIDs struct {
	Folder string
	UIDs   []uint32
}

// SyncOutcome 是一次账户同步的结果（对应 Rust SyncOutcome）。
type SyncOutcome struct {
	AccountID      string
	Emails         []Email
	Skipped        int
	HasMoreHistory bool
	Err            error
	Present        []FolderUIDs
}

func u32p(v uint32) *uint32 { return &v }
