package core

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// 备份与还原：把邮箱账户（含加密授权码）、同步游标、本地邮件和偏好设置
// 打包成一个口令加密的文件，方便换机迁移与数据备份。
//
// 文件格式（小端）：
//   偏移 0..6   magic "VELABAK"
//   偏移 7      格式版本（当前 1）
//   偏移 8..23  PBKDF2 盐（16 字节随机）
//   偏移 24..35 AES-GCM nonce（12 字节随机）
//   偏移 36..   AES-256-GCM 密文（内容为 JSON 信封）
//
// 口令经 PBKDF2-HMAC-SHA256（60 万次迭代）派生密钥。信封里带有 mail.key 的
// 密钥材料——授权码本身在数据库里是密文，整包再加密一层，因此备份文件
// 丢了（没有口令）不会泄露授权码；拿到口令的人可以在新设备上完整还原。
const (
	backupMagic   = "VELABAK"
	backupVersion = 1
	backupSaltLen = 16
	backupKeyLen  = 32
)

// backupKDFIters 是 PBKDF2 迭代次数（OWASP 2023 对 PBKDF2-SHA256 的建议值）。
// 测试里会临时调小以保持速度。
var backupKDFIters = 600_000

// backupAccount 是账户表的原始行（授权码保持密文原样迁移）。
type backupAccount struct {
	ID           string          `json:"id"`
	Email        string          `json:"email"`
	Provider     string          `json:"provider"`
	Label        string          `json:"label"`
	IMAPHost     string          `json:"imapHost"`
	IMAPPort     int             `json:"imapPort"`
	IMAPSecurity string          `json:"imapSecurity"`
	SMTPHost     string          `json:"smtpHost"`
	SMTPPort     int             `json:"smtpPort"`
	SMTPSecurity string          `json:"smtpSecurity"`
	SecretEnc    string          `json:"secretEnc"`
	Cursors      json.RawMessage `json:"cursors,omitempty"`
}

// backupEnvelope 是备份密文里的 JSON 信封。
type backupEnvelope struct {
	App       string          `json:"app"`
	Format    int             `json:"format"`
	CreatedAt string          `json:"createdAt"`
	Secret    string          `json:"secret"`
	Settings  json.RawMessage `json:"settings,omitempty"`
	Accounts  []backupAccount `json:"accounts"`
	Emails    []Email         `json:"emails"`
}

// RestoreResult 是还原后的摘要，界面据此刷新状态。
type RestoreResult struct {
	Accounts      int
	Emails        int
	SecretChanged bool
	Secret        string          // 备份携带的密钥材料（SecretChanged 时需写回 mail.key）
	Settings      json.RawMessage // 备份携带的偏好设置
}

/* ———— 数据库侧 ———— */

// ExportAccountsRaw 读取账户原始行（含密文授权码与同步游标）。
func (d *DB) ExportAccountsRaw() ([]backupAccount, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.db.Query(`SELECT id, email, provider, label, imap_host, imap_port, imap_security,
		smtp_host, smtp_port, smtp_security, secret_enc, cursors FROM mailbox_accounts ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("读取邮箱账户失败：%v", err)
	}
	defer rows.Close()
	var out []backupAccount
	for rows.Next() {
		var a backupAccount
		var cursors string
		if err := rows.Scan(&a.ID, &a.Email, &a.Provider, &a.Label, &a.IMAPHost, &a.IMAPPort, &a.IMAPSecurity,
			&a.SMTPHost, &a.SMTPPort, &a.SMTPSecurity, &a.SecretEnc, &cursors); err != nil {
			return nil, err
		}
		if cursors != "" {
			a.Cursors = json.RawMessage(cursors)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReplaceAll 在一个事务里清空并写入备份的账户与邮件（原子替换全部数据，
// 还原到一台已有账户的机器时覆盖旧账户而不是撞主键）。
func (d *DB) ReplaceAll(accounts []backupAccount, emails []Email) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM mailbox_accounts"); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec("DELETE FROM emails"); err != nil {
		tx.Rollback()
		return err
	}
	accStmt, err := tx.Prepare(`INSERT INTO mailbox_accounts
		(id, email, provider, label, imap_host, imap_port, imap_security, smtp_host, smtp_port, smtp_security, secret_enc, cursors)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer accStmt.Close()
	for _, a := range accounts {
		cursors := string(a.Cursors)
		if cursors == "" {
			cursors = "[]"
		}
		if _, err := accStmt.Exec(a.ID, a.Email, a.Provider, a.Label, a.IMAPHost, a.IMAPPort, a.IMAPSecurity,
			a.SMTPHost, a.SMTPPort, a.SMTPSecurity, a.SecretEnc, cursors); err != nil {
			tx.Rollback()
			return fmt.Errorf("写入邮箱账户失败：%v", err)
		}
	}
	if err := d.upsertEmailsTx(tx, emails); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

/* ———— 加密封装 ———— */

func deriveBackupKey(passphrase string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, backupKDFIters, backupKeyLen)
}

// sealBackup 把 JSON 信封按文件格式加密封装。
func sealBackup(payload []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, backupSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := deriveBackupKey(passphrase, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(backupMagic)+1+backupSaltLen+gcm.NonceSize()+len(payload)+gcm.Overhead())
	out = append(out, backupMagic...)
	out = append(out, byte(backupVersion))
	out = append(out, salt...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, payload, nil), nil
}

// openBackup 校验文件头并解密出 JSON 信封。
func openBackup(data []byte, passphrase string) ([]byte, error) {
	if len(data) < len(backupMagic)+1+backupSaltLen+12 {
		return nil, errors.New("不是有效的 Vela 备份文件")
	}
	if !bytes.Equal(data[:len(backupMagic)], []byte(backupMagic)) {
		return nil, errors.New("不是有效的 Vela 备份文件")
	}
	if v := data[len(backupMagic)]; v != backupVersion {
		return nil, fmt.Errorf("备份格式版本 %d 无法读取（当前支持 %d）", v, backupVersion)
	}
	salt := data[len(backupMagic)+1 : len(backupMagic)+1+backupSaltLen]
	rest := data[len(backupMagic)+1+backupSaltLen:]
	key, err := deriveBackupKey(passphrase, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(rest) < gcm.NonceSize() {
		return nil, errors.New("备份文件已损坏")
	}
	payload, err := gcm.Open(nil, rest[:gcm.NonceSize()], rest[gcm.NonceSize():], nil)
	if err != nil {
		return nil, errors.New("密码错误或备份文件已损坏")
	}
	return payload, nil
}

/* ———— 对界面暴露的能力 ———— */

// ExportBackup 生成整包加密备份：账户（密文原样）+ 邮件 + 同步游标 + 偏好设置。
func (b *Backend) ExportBackup(passphrase string, settings json.RawMessage) ([]byte, error) {
	if len(passphrase) < 4 {
		return nil, errors.New("备份密码至少需要 4 个字符")
	}
	accounts, err := b.DB.ExportAccountsRaw()
	if err != nil {
		return nil, err
	}
	emails, err := b.DB.GetEmails()
	if err != nil {
		return nil, err
	}
	if accounts == nil {
		accounts = []backupAccount{}
	}
	if emails == nil {
		emails = []Email{}
	}
	env := backupEnvelope{
		App:       "vela",
		Format:    backupVersion,
		CreatedAt: time.Now().Format(time.RFC3339),
		Secret:    b.Secret(),
		Settings:  settings,
		Accounts:  accounts,
		Emails:    emails,
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("序列化备份数据失败：%v", err)
	}
	return sealBackup(payload, passphrase)
}

// ImportBackup 解密备份并原子替换全部账户与邮件；密钥与本地不同时热切换
// 加密器（监控与同步立即使用新密钥，无需重启）。
func (b *Backend) ImportBackup(passphrase string, data []byte) (*RestoreResult, error) {
	payload, err := openBackup(data, passphrase)
	if err != nil {
		return nil, err
	}
	var env backupEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, errors.New("备份文件内容损坏（无法解析）")
	}
	if env.Format > backupVersion {
		return nil, fmt.Errorf("备份由更新版本的 Vela 导出（格式 %d）", env.Format)
	}
	if env.Secret == "" {
		return nil, errors.New("备份缺少密钥材料，无法还原授权码")
	}
	if err := b.DB.ReplaceAll(env.Accounts, env.Emails); err != nil {
		return nil, err
	}
	changed := false
	if old := b.Secret(); old != env.Secret {
		b.setCrypto(env.Secret, NewCrypto(env.Secret))
		changed = true
	}
	return &RestoreResult{
		Accounts:      len(env.Accounts),
		Emails:        len(env.Emails),
		SecretChanged: changed,
		Secret:        env.Secret,
		Settings:      env.Settings,
	}, nil
}
