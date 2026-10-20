package core

import (
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// OutgoingMail 是要发送的一封邮件（对应 Rust OutgoingMail）。
type OutgoingMail struct {
	To        []string
	CC        []string
	InReplyTo string
	Subject   string
	Body      string
}

// ParseMailbox 解析 "名字 <a@b.com>" 或 "a@b.com"。
func ParseMailbox(raw string) (*mail.Address, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("收件人为空")
	}
	if l, r := strings.IndexByte(s, '<'), strings.LastIndexByte(s, '>'); l >= 0 && r > l {
		name := strings.Trim(strings.TrimSpace(s[:l]), `"`)
		addr := strings.TrimSpace(s[l+1 : r])
		if !validAddr(addr) {
			return nil, fmt.Errorf("邮箱地址不合法：%s", addr)
		}
		return &mail.Address{Name: name, Address: addr}, nil
	}
	if !validAddr(s) {
		return nil, fmt.Errorf("邮箱地址不合法：%s", s)
	}
	return &mail.Address{Address: s}, nil
}

func validAddr(a string) bool {
	if strings.ContainsAny(a, " \t<>\"") {
		return false
	}
	at := strings.LastIndexByte(a, '@')
	return at > 0 && at < len(a)-1
}

// BuildMessage 构造 RFC 5322 纯文本邮件。
func BuildMessage(from *mail.Address, to, cc []*mail.Address, inReplyTo, subject, body string, now time.Time) []byte {
	join := func(l []*mail.Address) string {
		parts := make([]string, len(l))
		for i, a := range l {
			parts[i] = a.String()
		}
		return strings.Join(parts, ", ")
	}
	var b strings.Builder
	w := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	w("From", from.String())
	w("To", join(to))
	if len(cc) > 0 {
		w("Cc", join(cc))
	}
	w("Subject", mime.QEncoding.Encode("utf-8", subject))
	w("Date", now.Format(time.RFC1123Z))
	host := from.Address[strings.LastIndexByte(from.Address, '@')+1:]
	w("Message-ID", fmt.Sprintf("<%d.vela@%s>", now.UnixNano(), host))
	if inReplyTo != "" {
		id := strings.TrimSpace(inReplyTo)
		if !strings.HasPrefix(id, "<") {
			id = "<" + id + ">"
		}
		w("In-Reply-To", id)
		w("References", id)
	}
	w("MIME-Version", "1.0")
	w("Content-Type", "text/plain; charset=utf-8")
	w("Content-Transfer-Encoding", "8bit")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	return []byte(b.String())
}

// Send 通过 SMTP 发送（对应 Rust smtp::send），成功返回服务器回执说明。
func Send(cfg MailServerConfig, m OutgoingMail) (string, error) {
	local := cfg.Email
	if i := strings.IndexByte(local, '@'); i >= 0 {
		local = local[:i]
	}
	from := &mail.Address{Name: local, Address: cfg.Email}
	var to, cc []*mail.Address
	for _, r := range m.To {
		a, err := ParseMailbox(r)
		if err != nil {
			return "", err
		}
		to = append(to, a)
	}
	if len(to) == 0 {
		return "", errors.New("请至少填写一个收件人")
	}
	for _, r := range m.CC {
		a, err := ParseMailbox(r)
		if err != nil {
			return "", err
		}
		cc = append(cc, a)
	}
	if cfg.SMTPSecurity == SecurityNone && !allowInsecure {
		return "", errors.New("出于安全考虑不支持无加密的 SMTP；请使用 465(SSL) 或 587(STARTTLS)")
	}
	msg := BuildMessage(from, to, cc, m.InReplyTo, m.Subject, m.Body, time.Now())

	addr := net.JoinHostPort(cfg.SMTPHost, fmt.Sprint(cfg.SMTPPort))
	tlsCfg := &tls.Config{ServerName: cfg.SMTPHost}
	var c *smtp.Client
	var err error
	switch cfg.SMTPSecurity {
	case SecuritySSL:
		c, err = smtp.DialTLS(addr, tlsCfg)
	case SecurityStartTLS:
		c, err = smtp.DialStartTLS(addr, tlsCfg)
	default:
		c, err = smtp.Dial(addr)
	}
	if err != nil {
		return "", smtpError(err)
	}
	defer c.Close()
	c.CommandTimeout = 60 * time.Second
	if err := c.Auth(sasl.NewPlainClient("", cfg.Email, cfg.Password)); err != nil {
		return "", smtpError(err)
	}
	var rcpts []string
	for _, a := range append(append([]*mail.Address{}, to...), cc...) {
		rcpts = append(rcpts, a.Address)
	}
	if err := c.SendMail(cfg.Email, rcpts, strings.NewReader(string(msg))); err != nil {
		return "", smtpError(err)
	}
	_ = c.Quit()
	return "已投递", nil
}

func smtpError(err error) error {
	text := err.Error()
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "auth") || strings.Contains(lower, "535") || strings.Contains(lower, "credentials"):
		return fmt.Errorf("SMTP 认证失败：请确认使用的是授权码 / 应用专用密码（%s）", text)
	case strings.Contains(lower, "relay") || strings.Contains(lower, "550"):
		return fmt.Errorf("服务器拒绝投递：%s（可能被判定为垃圾邮件或收件人不存在）", text)
	}
	return fmt.Errorf("发送失败：%s", text)
}
