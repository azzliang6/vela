package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Crypto 用 AES-256-GCM 加解密邮箱授权码，格式与 Rust 版完全一致：
// key = SHA-256(secret)，密文 = base64(nonce(12) ‖ ciphertext ‖ tag)。
type Crypto struct{ aead cipher.AEAD }

// NewCrypto 从任意长度的密钥材料构造加密器。
func NewCrypto(secret string) *Crypto {
	key := sha256.Sum256([]byte(secret))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	return &Crypto{aead: aead}
}

// Encrypt 加密明文。
func (c *Crypto) Encrypt(plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errors.New("加密失败")
	}
	return base64.StdEncoding.EncodeToString(c.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Decrypt 解密 Encrypt 的输出（也能解 Rust 版写入的密文）。
func (c *Crypto) Decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", errors.New("密文格式损坏（无法解码）")
	}
	n := c.aead.NonceSize()
	if len(raw) <= n {
		return "", errors.New("密文长度不足")
	}
	plain, err := c.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", errors.New("无法解密（密钥已变更或数据被篡改），请重新输入邮箱授权码")
	}
	if !utf8.Valid(plain) {
		return "", errors.New("解密结果不是合法文本")
	}
	return string(plain), nil
}

// LoadSecret 依次读取 VELA_SECRET 环境变量和 dir/mail.key。
func LoadSecret(dir string) (string, bool) {
	if v := strings.TrimSpace(os.Getenv("VELA_SECRET")); v != "" {
		return v, true
	}
	if dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, "mail.key")); err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				return v, true
			}
		}
	}
	return "", false
}

// GenerateSecretFile 生成随机密钥写入 dir/mail.key（权限 0600）。
func GenerateSecretFile(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建配置目录：%v", err)
	}
	secret, err := newSecret()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "mail.key"), []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("无法写入密钥文件：%v", err)
	}
	return secret, nil
}

// newSecret 生成 32 字节随机密钥（base64）。
func newSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// LoadCrypto 取得加密器，必要时首次生成 mail.key。
func LoadCrypto(configDir string) (*Crypto, error) {
	if s, ok := LoadSecret(configDir); ok {
		return NewCrypto(s), nil
	}
	if configDir == "" {
		return nil, errors.New("无法确定密钥存储位置，请设置 VELA_SECRET 环境变量")
	}
	s, err := GenerateSecretFile(configDir)
	if err != nil {
		return nil, err
	}
	return NewCrypto(s), nil
}
