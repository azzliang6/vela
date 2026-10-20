// Package core 是 Vela 的本地后端：服务商预设、授权码加密、SQLite 本地库、
// IMAP 收信、SMTP 发信、IDLE 新邮件监控和 AI 调用。
//
// 它由原 Tauri 版的 Rust 代码（src-tauri/src）逐模块移植而来，
// 行为、错误提示和磁盘数据格式都与 Rust 版保持一致。
package core

import (
	"errors"
	"fmt"
	"strings"
)

// Security 是传输加密方式。
type Security string

const (
	SecuritySSL      Security = "ssl"
	SecurityStartTLS Security = "starttls"
	SecurityNone     Security = "none"
)

// MailServerConfig 是一个邮箱账户的完整连接参数。
type MailServerConfig struct {
	Provider     string
	Email        string
	IMAPHost     string
	IMAPPort     int
	IMAPSecurity Security
	SMTPHost     string
	SMTPPort     int
	SMTPSecurity Security
	// Password 是授权码，只在内存中短暂存在，落库前加密。
	Password string
}

// ProviderPreset 是展示给用户的服务商预设。
type ProviderPreset struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	IMAPHost string   `json:"imapHost"`
	IMAPPort int      `json:"imapPort"`
	SMTPHost string   `json:"smtpHost"`
	SMTPPort int      `json:"smtpPort"`
	Hint     string   `json:"hint"`
	Domains  []string `json:"domains"`
}

var presets = []ProviderPreset{
	{"qq", "QQ 邮箱", "imap.qq.com", 993, "smtp.qq.com", 465, "QQ 邮箱需在「设置 → 账户 → POP3/IMAP/SMTP 服务」开启 IMAP 并生成 16 位授权码，登录时用它代替 QQ 密码", []string{"qq.com", "vip.qq.com", "foxmail.com"}},
	{"163", "网易 163", "imap.163.com", 993, "smtp.163.com", 465, "163 邮箱在「设置 → POP3/SMTP/IMAP」开启 IMAP/SMTP 后生成授权码；首次连接可能需在网页端确认客户端授权", []string{"163.com"}},
	{"126", "网易 126", "imap.126.com", 993, "smtp.126.com", 465, "126 邮箱与 163 相同，需开启 IMAP/SMTP 并使用授权码登录", []string{"126.com"}},
	{"yeah", "网易 yeah.net", "imap.yeah.net", 993, "smtp.yeah.net", 465, "yeah.net 邮箱需开启 IMAP/SMTP 并使用授权码登录", []string{"yeah.net"}},
	{"gmail", "Gmail", "imap.gmail.com", 993, "smtp.gmail.com", 465, "Gmail 需开启两步验证后生成「应用专用密码」，用它作为密码；普通账号密码无法通过 IMAP 登录。国内网络需能访问 Google 服务", []string{"gmail.com", "googlemail.com"}},
	{"outlook", "Outlook / Hotmail", "outlook.office365.com", 993, "smtp.office365.com", 587, "Outlook 个人账户可直接用账户密码；若开启了双重验证，需生成「应用密码」。企业账户需管理员开启 IMAP", []string{"outlook.com", "hotmail.com", "live.com", "msn.com", "outlook.jp"}},
	{"icloud", "iCloud", "imap.mail.me.com", 993, "smtp.mail.me.com", 587, "iCloud 需在 Apple ID 设置中生成「App 专用密码」，账号名填完整邮箱地址", []string{"icloud.com", "me.com", "mac.com"}},
	{"yahoo", "Yahoo 邮箱", "imap.mail.yahoo.com", 993, "smtp.mail.yahoo.com", 465, "Yahoo 需在账户安全设置中生成「应用密码」", []string{"yahoo.com", "yahoo.co.jp", "ymail.com"}},
	{"exmail", "腾讯企业邮箱", "imap.exmail.qq.com", 993, "smtp.exmail.qq.com", 465, "企业邮箱通常可直接用登录密码，若管理员开启了安全设置则需在网页端生成专用密码", []string{"exmail.qq.com"}},
	{"aliyun", "阿里云邮箱", "imap.mxhichina.com", 993, "smtp.mxhichina.com", 465, "阿里云企业邮箱使用完整邮箱地址作为用户名，密码为邮箱登录密码", []string{"mxhichina.com", "aliyun.com"}},
	{"sina", "新浪邮箱", "imap.sina.com", 993, "smtp.sina.com", 465, "新浪邮箱需在「设置 → 客户端 POP/IMAP」中开启 IMAP 服务", []string{"sina.com", "sina.cn"}},
	{"custom", "自定义 / 自建服务器", "", 993, "", 465, "手动填写 IMAP 与 SMTP 服务器地址。常见约定：IMAP 993(SSL) 或 143(STARTTLS)；SMTP 465(SSL) 或 587(STARTTLS)", nil},
}

// AllPresets 返回全部服务商预设（对应 Rust mail_providers）。
func AllPresets() []ProviderPreset {
	out := make([]ProviderPreset, len(presets))
	copy(out, presets)
	return out
}

// PresetByID 返回 id 对应的预设。
func PresetByID(id string) (ProviderPreset, bool) {
	for _, p := range presets {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderPreset{}, false
}

// GuessProvider 按邮箱域名猜服务商，匹配不到返回 "custom"。
func GuessProvider(email string) string {
	domain := ""
	if i := strings.LastIndexByte(email, '@'); i >= 0 {
		domain = strings.ToLower(email[i+1:])
	}
	for _, p := range presets {
		for _, d := range p.Domains {
			if d == domain {
				return p.ID
			}
		}
	}
	return "custom"
}

// ApplyPreset 用预设补齐服务器地址并按端口推断加密方式。
func ApplyPreset(cfg *MailServerConfig) error {
	if strings.TrimSpace(cfg.Email) == "" || !strings.Contains(cfg.Email, "@") {
		return errors.New("请填写完整的邮箱地址")
	}
	p, ok := PresetByID(cfg.Provider)
	if !ok {
		return fmt.Errorf("未知的服务商：%s", cfg.Provider)
	}
	if p.ID == "custom" {
		if strings.TrimSpace(cfg.IMAPHost) == "" || strings.TrimSpace(cfg.SMTPHost) == "" {
			return errors.New("自定义服务商需要填写 IMAP 与 SMTP 服务器地址")
		}
	} else {
		cfg.IMAPHost, cfg.IMAPPort = p.IMAPHost, p.IMAPPort
		cfg.SMTPHost, cfg.SMTPPort = p.SMTPHost, p.SMTPPort
	}
	switch cfg.IMAPPort {
	case 993:
		cfg.IMAPSecurity = SecuritySSL
	case 143:
		cfg.IMAPSecurity = SecurityStartTLS
	}
	switch cfg.SMTPPort {
	case 465:
		cfg.SMTPSecurity = SecuritySSL
	case 587, 25:
		cfg.SMTPSecurity = SecurityStartTLS
	}
	return nil
}

// FolderKind 是服务端文件夹类型。
type FolderKind int

const (
	FolderSent FolderKind = iota
	FolderArchive
	FolderTrash
	FolderDrafts
)

// FolderCandidates 返回某类文件夹在各服务商的常见名字。
func FolderCandidates(k FolderKind) []string {
	switch k {
	case FolderSent:
		return []string{"Sent", "Sent Messages", "Sent Items", "已发送", "已发送邮件", "[Gmail]/Sent Mail", "[Google Mail]/Sent Mail", "Sent Mail", "&XfJT0ZAB-"}
	case FolderArchive:
		return []string{"Archive", "Archives", "归档", "所有邮件", "[Gmail]/All Mail"}
	case FolderTrash:
		return []string{"Trash", "Deleted Messages", "Deleted Items", "已删除", "已删除邮件", "[Gmail]/Trash", "&XfJSIJZk-"}
	default:
		return []string{"Drafts", "草稿箱", "草稿", "[Gmail]/Drafts"}
	}
}

// AddAccountInput 是新增账户的参数（对应 Rust AddAccountInput）。
type AddAccountInput struct {
	Provider string
	Email    string
	Password string
	Label    string
	IMAPHost string
	IMAPPort int
	SMTPHost string
	SMTPPort int
}

// Config 把输入转成连接配置并校验。
func (in AddAccountInput) Config() (MailServerConfig, error) {
	cfg := MailServerConfig{
		Provider: strings.TrimSpace(in.Provider), Email: strings.TrimSpace(in.Email),
		IMAPHost: strings.TrimSpace(in.IMAPHost), IMAPPort: in.IMAPPort, IMAPSecurity: SecuritySSL,
		SMTPHost: strings.TrimSpace(in.SMTPHost), SMTPPort: in.SMTPPort, SMTPSecurity: SecuritySSL,
		Password: in.Password,
	}
	if cfg.IMAPPort == 0 {
		cfg.IMAPPort = 993
	}
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 465
	}
	if err := ApplyPreset(&cfg); err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.Password) == "" {
		return cfg, errors.New("请填写邮箱授权码 / 密码")
	}
	return cfg, nil
}
