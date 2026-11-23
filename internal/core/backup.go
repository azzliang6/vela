package core

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 备份与还原：把邮箱账户（含加密授权码）、同步游标、本地邮件和偏好设置
// 打包成一个口令加密的文件，方便换机迁移与数据备份。
//
// 文件格式：
//
//	偏移 0..6   magic "VELABAK"
//	偏移 7      格式版本（当前 2；1 为未压缩的旧格式，仍可读取）
//	偏移 8..23  PBKDF2 盐（16 字节随机）
//	偏移 24..35 AES-GCM nonce（12 字节随机）
//	偏移 36..   AES-256-GCM 密文（内容为 gzip 压缩的 JSON 信封；版本 1 未压缩）
//
// 口令经 PBKDF2-HMAC-SHA256（60 万次迭代）派生密钥。信封里带有 mail.key 的
// 密钥材料——授权码本身在数据库里是密文，整包再加密一层，因此备份文件
// 丢了（没有口令）不会泄露授权码；拿到口令的人可以在新设备上完整还原。
//
// 还原不更换本机密钥：备份来自另一台设备时，先用备份携带的密钥材料解开
// 授权码，再用本机密钥（mail.key 或 VELA_SECRET）重新加密后写入。覆盖之前
// 会给本机当前数据留一份副本（见 DB.SnapshotBeforeRestore）。
const (
	backupMagic   = "VELABAK"
	backupVersion = 2 // 写出的格式版本
	backupV1      = 1 // 未压缩的旧格式
	// backupMaxJSON 是解压后 JSON 信封的上限，防止被构造的文件解压成天文数字
	backupMaxJSON = 4 << 30
	backupSaltLen = 16
	backupKeyLen  = 32
)

// backupKDFIters 是 PBKDF2 迭代次数（OWASP 2023 对 PBKDF2-SHA256 的建议值）。
// 测试里会临时调小以保持速度。
var backupKDFIters = 600_000

// backupAccount 是账户表的原始行。授权码是密文：备份的密钥材料与本机不同时，
// 还原前会换到本机密钥下（见 rekeyAccounts）。
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
	Accounts int
	Emails   int
	Settings json.RawMessage // 备份携带的偏好设置
	Snapshot string          // 覆盖前留下的本机数据副本路径；本机原本没有数据时为空
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

/* ———— 还原前的数据副本 ———— */

const (
	snapshotDirName = "snapshots"
	SnapshotKeep    = 3
	snapshotPrefix  = "vela-before-restore-"
)

// SnapshotBeforeRestore 在还原覆盖之前给当前数据留一份完整的副本，返回副本路径。
// 本机没有任何账户和邮件时不需要留底，返回空串。副本是独立的 SQLite 文件，
// 授权码仍是密文，密钥材料（mail.key）不在其中。
func (d *DB) SnapshotBeforeRestore() (string, error) {
	if d.path == "" || d.path == ":memory:" {
		return "", nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	var rows int
	if err := d.db.QueryRow(`SELECT (SELECT COUNT(*) FROM mailbox_accounts) + (SELECT COUNT(*) FROM emails)`).Scan(&rows); err != nil {
		return "", fmt.Errorf("读取本机数据失败：%v", err)
	}
	if rows == 0 {
		return "", nil
	}

	dir := filepath.Join(filepath.Dir(d.path), snapshotDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建还原副本目录：%v", err)
	}
	path, err := freeSnapshotPath(dir)
	if err != nil {
		return "", err
	}
	// 持有 d.mu 期间没有其它写入，VACUUM INTO 由 SQLite 保证写出一致的副本
	if _, err := d.db.Exec("VACUUM INTO ?", path); err != nil {
		return "", fmt.Errorf("无法创建还原前的数据副本（已取消还原，本机数据未改动）：%v", err)
	}
	_ = os.Chmod(path, 0o600)
	pruneSnapshots(dir)
	return path, nil
}

// freeSnapshotPath 选一个不冲突的副本文件名：vela-before-restore-<时间>.db。
func freeSnapshotPath(dir string) (string, error) {
	stamp := time.Now().Format("20060102-150405")
	for i := 1; ; i++ {
		name := fmt.Sprintf("%s%s.db", snapshotPrefix, stamp)
		if i > 1 {
			name = fmt.Sprintf("%s%s-%d.db", snapshotPrefix, stamp, i)
		}
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		if i >= 99 {
			return "", errors.New("还原副本目录里无法再创建新文件，请清理后重试")
		}
	}
}

// pruneSnapshots 只保留最近 SnapshotKeep 份还原前的副本。
func pruneSnapshots(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type snapFile struct {
		path string
		mod  time.Time
	}
	var snaps []snapFile
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), snapshotPrefix) || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // 刚被别处删掉
		}
		snaps = append(snaps, snapFile{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	// 按修改时间排：同一秒内的第二份副本文件名带序号后缀，字典序不再是时间序
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].mod.Before(snaps[j].mod) })
	for _, s := range snaps[:max(0, len(snaps)-SnapshotKeep)] {
		_ = os.Remove(s.path)
	}
}

/* ———— 加密封装 ———— */

func deriveBackupKey(passphrase string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, backupKDFIters, backupKeyLen)
}

// sealBackup 把 JSON 信封压缩后按文件格式加密封装。邮件正文压缩率很高，
// 备份文件通常只有原来的几分之一。必须先压缩再加密：密文无法再压缩。
func sealBackup(payload []byte, passphrase string) ([]byte, error) {
	var zbuf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&zbuf, gzip.BestCompression)
	if _, err := zw.Write(payload); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return sealBackupRaw(zbuf.Bytes(), passphrase, backupVersion)
}

// sealBackupRaw 按指定格式版本加密封装 payload（版本 1 为未压缩的 JSON）。
func sealBackupRaw(payload []byte, passphrase string, version byte) ([]byte, error) {
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
	out = append(out, version)
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
	version := data[len(backupMagic)]
	if version != backupVersion && version != backupV1 {
		return nil, fmt.Errorf("备份格式版本 %d 无法读取（请升级 Vela）", version)
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
	if version == backupV1 {
		return payload, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("备份文件已损坏（无法解压）")
	}
	out, err := io.ReadAll(io.LimitReader(zr, backupMaxJSON+1))
	if err != nil {
		return nil, errors.New("备份文件已损坏（无法解压）")
	}
	if len(out) > backupMaxJSON {
		return nil, errors.New("备份文件内容过大，无法还原")
	}
	return out, nil
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

// rekeyAccounts 把账户授权码的密文从 from（备份来源设备的密钥）换到 to（本机密钥）下。
// 任何一个解不开都整体报错，此时尚未写入任何数据。
func rekeyAccounts(accounts []backupAccount, from, to *Crypto) error {
	for i := range accounts {
		pw, err := from.Decrypt(accounts[i].SecretEnc)
		if err != nil {
			return fmt.Errorf("备份里 %s 的授权码无法解密（备份来源设备的密钥与数据对不上）；已取消还原，本机数据未改动",
				accounts[i].Email)
		}
		enc, err := to.Encrypt(pw)
		if err != nil {
			return err
		}
		accounts[i].SecretEnc = enc
	}
	return nil
}

// ImportBackup 解密备份并原子替换全部账户与邮件。本机密钥保持不变：备份来自
// 别的密钥时，授权码密文先换到本机密钥下再写入。全部步骤都成功后才让后台
// 监控按新的账户列表重连。
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
	if env.Secret != b.secret {
		if err := rekeyAccounts(env.Accounts, NewCrypto(env.Secret), b.crypto); err != nil {
			return nil, err
		}
	}
	snapshot, err := b.DB.SnapshotBeforeRestore()
	if err != nil {
		return nil, err
	}
	if err := b.DB.ReplaceAll(env.Accounts, env.Emails); err != nil {
		return nil, err
	}
	if w := b.watcher.Load(); w != nil {
		w.Rescan()
	}
	return &RestoreResult{
		Accounts: len(env.Accounts),
		Emails:   len(env.Emails),
		Settings: env.Settings,
		Snapshot: snapshot,
	}, nil
}
