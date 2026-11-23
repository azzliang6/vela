package core

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

// 授权码加密密钥优先存放在系统钥匙串里（macOS 钥匙串、Windows 凭据管理器、
// Linux Secret Service），而不是数据目录旁边的明文 mail.key。
//
// 读取顺序：VELA_SECRET 环境变量 → 系统钥匙串 → mail.key 文件。
// 迁移：钥匙串可用而密钥还在 mail.key 里时，写进钥匙串并读回核对一致后才删除
// 文件，同时留下一个不含密钥的标记文件 mail.key.keychain。之后若钥匙串暂时读
// 不到（被锁、服务没起来），看到标记就报错让用户重试，绝不悄悄生成新密钥——
// 那样已保存的授权码就再也解不开了。
// 设置了 VELA_DATA_DIR（便携/测试用的自定义数据目录）或 VELA_NO_KEYCHAIN 时
// 不使用钥匙串：钥匙串条目是全机共享的，不能和某个自定义目录绑定。

const (
	keychainService = Identifier
	keychainUser    = "mail.key"
	keychainMarker  = "mail.key.keychain"
	keychainTimeout = 5 * time.Second
)

// SecretSource 说明当前密钥来自哪里。
type SecretSource string

const (
	SecretFromEnv      SecretSource = "env"
	SecretFromKeychain SecretSource = "keychain"
	SecretFromFile     SecretSource = "file"
)

// errKeychainEmpty 表示钥匙串可用但里面还没有 Vela 的密钥。
var errKeychainEmpty = errors.New("钥匙串中没有密钥")

// secretStore 是系统钥匙串的抽象，测试时替换成内存实现。
type secretStore interface {
	Get() (string, error) // 没有条目时返回 errKeychainEmpty
	Set(secret string) error
}

var keychain secretStore = systemKeychain{}

type systemKeychain struct{}

func (systemKeychain) Get() (string, error) {
	var s string
	err := withTimeout(func() error {
		var err error
		s, err = keyring.Get(keychainService, keychainUser)
		return err
	})
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errKeychainEmpty
	}
	return s, err
}

func (systemKeychain) Set(secret string) error {
	return withTimeout(func() error { return keyring.Set(keychainService, keychainUser, secret) })
}

// withTimeout 防止钥匙串弹出解锁框或服务无响应时卡住启动。
func withTimeout(fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(keychainTimeout):
		return errors.New("系统钥匙串无响应")
	}
}

func keychainEnabled() bool {
	return os.Getenv("VELA_DATA_DIR") == "" && os.Getenv("VELA_NO_KEYCHAIN") == ""
}

// loadOrCreateSecret 按上面的规则取得密钥，必要时迁移或首次生成。
func loadOrCreateSecret(dir string) (string, SecretSource, error) {
	if v := strings.TrimSpace(os.Getenv("VELA_SECRET")); v != "" {
		return v, SecretFromEnv, nil
	}
	keyFile := filepath.Join(dir, "mail.key")
	marker := filepath.Join(dir, keychainMarker)

	kcUsable := false // 钥匙串能正常读写（有没有条目都算）
	if keychainEnabled() {
		s, err := keychain.Get()
		switch {
		case err == nil && strings.TrimSpace(s) != "":
			return strings.TrimSpace(s), SecretFromKeychain, nil
		case err == nil || errors.Is(err, errKeychainEmpty):
			kcUsable = true
		default:
			log.Printf("[vela] 系统钥匙串不可用，改用密钥文件：%v", err)
		}
	}

	if b, err := os.ReadFile(keyFile); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			if kcUsable && moveToKeychain(s) {
				_ = os.WriteFile(marker, []byte(markerNote), 0o600)
				if err := os.Remove(keyFile); err != nil {
					log.Printf("[vela] 密钥已存入钥匙串，但旧的 mail.key 未能删除：%v", err)
				}
				return s, SecretFromKeychain, nil
			}
			return s, SecretFromFile, nil
		}
	}

	if _, err := os.Stat(marker); err == nil {
		// 密钥已经迁进钥匙串，现在却读不到：不能生成新的，否则旧授权码全部作废
		return "", "", errors.New("无法从系统钥匙串读取 Vela 的加密密钥（钥匙串可能被锁定）。请解锁钥匙串后重新打开 Vela；" +
			"如需改回密钥文件，可设置 VELA_SECRET 环境变量")
	}

	if dir == "" {
		return "", "", errors.New("无法确定密钥存储位置，请设置 VELA_SECRET 环境变量")
	}
	if kcUsable {
		s, err := newSecret()
		if err != nil {
			return "", "", err
		}
		if moveToKeychain(s) {
			if err := os.MkdirAll(dir, 0o755); err == nil {
				_ = os.WriteFile(marker, []byte(markerNote), 0o600)
			}
			return s, SecretFromKeychain, nil
		}
	}
	s, err := GenerateSecretFile(dir)
	if err != nil {
		return "", "", err
	}
	return s, SecretFromFile, nil
}

// moveToKeychain 写入钥匙串并读回核对，确认真的存住了才返回 true。
func moveToKeychain(secret string) bool {
	if err := keychain.Set(secret); err != nil {
		log.Printf("[vela] 无法把密钥存入系统钥匙串：%v", err)
		return false
	}
	got, err := keychain.Get()
	if err != nil || strings.TrimSpace(got) != secret {
		log.Printf("[vela] 密钥存入系统钥匙串后读回不一致，继续使用密钥文件")
		return false
	}
	return true
}

const markerNote = "Vela 的授权码加密密钥保存在系统钥匙串中（服务名 " + keychainService + "）。\n" +
	"此文件不含密钥，只用来提醒程序：钥匙串读不到时不要生成新密钥。\n"
