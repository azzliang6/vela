package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// memKeychain 是内存里的钥匙串；broken 模拟被锁定/服务不可用，lies 模拟写入后读不回来。
type memKeychain struct {
	val    string
	broken bool
	lies   bool
}

func (m *memKeychain) Get() (string, error) {
	if m.broken {
		return "", errors.New("钥匙串已锁定")
	}
	if m.val == "" {
		return "", errKeychainEmpty
	}
	return m.val, nil
}

func (m *memKeychain) Set(s string) error {
	if m.broken {
		return errors.New("钥匙串已锁定")
	}
	if !m.lies {
		m.val = s
	}
	return nil
}

func keychainEnv(t *testing.T, kc *memKeychain) string {
	t.Helper()
	t.Setenv("VELA_DATA_DIR", "")
	t.Setenv("VELA_SECRET", "")
	t.Setenv("VELA_NO_KEYCHAIN", "")
	old := keychain
	keychain = kc
	t.Cleanup(func() { keychain = old })
	return t.TempDir()
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestKeychainFreshInstall(t *testing.T) {
	kc := &memKeychain{}
	dir := keychainEnv(t, kc)
	s, src, err := loadOrCreateSecret(dir)
	if err != nil || src != SecretFromKeychain || s == "" || kc.val != s {
		t.Fatalf("首次启动应在钥匙串里生成密钥：%v %v", err, src)
	}
	if exists(filepath.Join(dir, "mail.key")) || !exists(filepath.Join(dir, keychainMarker)) {
		t.Fatal("不应写明文 mail.key，应留下标记文件")
	}
	again, src2, _ := loadOrCreateSecret(dir)
	if again != s || src2 != SecretFromKeychain {
		t.Fatal("再次启动应读到同一把密钥")
	}
}

func TestKeychainMigratesKeyFile(t *testing.T) {
	kc := &memKeychain{}
	dir := keychainEnv(t, kc)
	_ = os.WriteFile(filepath.Join(dir, "mail.key"), []byte("老密钥\n"), 0o600)
	s, src, err := loadOrCreateSecret(dir)
	if err != nil || s != "老密钥" || src != SecretFromKeychain || kc.val != "老密钥" {
		t.Fatalf("应把旧密钥迁进钥匙串：%q %v %v", s, src, err)
	}
	if exists(filepath.Join(dir, "mail.key")) {
		t.Fatal("迁移成功后应删除明文 mail.key")
	}
}

func TestKeychainUnavailableKeepsFile(t *testing.T) {
	kc := &memKeychain{broken: true}
	dir := keychainEnv(t, kc)
	_ = os.WriteFile(filepath.Join(dir, "mail.key"), []byte("文件密钥"), 0o600)
	s, src, err := loadOrCreateSecret(dir)
	if err != nil || s != "文件密钥" || src != SecretFromFile || !exists(filepath.Join(dir, "mail.key")) {
		t.Fatalf("钥匙串不可用时应继续用文件：%q %v %v", s, src, err)
	}
}

func TestKeychainUnavailableFreshFallsBackToFile(t *testing.T) {
	dir := keychainEnv(t, &memKeychain{broken: true})
	s, src, err := loadOrCreateSecret(dir)
	if err != nil || src != SecretFromFile || s == "" || !exists(filepath.Join(dir, "mail.key")) {
		t.Fatalf("没有钥匙串的新环境应生成 mail.key：%v %v", src, err)
	}
}

// 已迁进钥匙串、此刻却读不到：报错，绝不生成新密钥。
func TestKeychainLockedAfterMigrationRefuses(t *testing.T) {
	kc := &memKeychain{}
	dir := keychainEnv(t, kc)
	if _, _, err := loadOrCreateSecret(dir); err != nil {
		t.Fatal(err)
	}
	kc.broken = true
	_, _, err := loadOrCreateSecret(dir)
	if err == nil {
		t.Fatal("钥匙串读不到时应报错")
	}
	if exists(filepath.Join(dir, "mail.key")) {
		t.Fatal("不应生成新的 mail.key")
	}
}

// 写入钥匙串后读不回来：不删文件，继续用文件。
func TestKeychainWriteNotVerifiedKeepsFile(t *testing.T) {
	dir := keychainEnv(t, &memKeychain{lies: true})
	_ = os.WriteFile(filepath.Join(dir, "mail.key"), []byte("文件密钥"), 0o600)
	s, src, err := loadOrCreateSecret(dir)
	if err != nil || s != "文件密钥" || src != SecretFromFile || !exists(filepath.Join(dir, "mail.key")) ||
		exists(filepath.Join(dir, keychainMarker)) {
		t.Fatalf("写入未核实时不应删除文件：%v %v", src, err)
	}
}

func TestSecretEnvAndOptOut(t *testing.T) {
	kc := &memKeychain{val: "钥匙串密钥"}
	dir := keychainEnv(t, kc)
	t.Setenv("VELA_SECRET", "环境变量密钥")
	if s, src, _ := loadOrCreateSecret(dir); s != "环境变量密钥" || src != SecretFromEnv {
		t.Fatalf("VELA_SECRET 优先：%q %v", s, src)
	}
	t.Setenv("VELA_SECRET", "")
	t.Setenv("VELA_NO_KEYCHAIN", "1")
	_ = os.WriteFile(filepath.Join(dir, "mail.key"), []byte("文件密钥"), 0o600)
	if s, src, _ := loadOrCreateSecret(dir); s != "文件密钥" || src != SecretFromFile {
		t.Fatalf("VELA_NO_KEYCHAIN 时只用文件：%q %v", s, src)
	}
	t.Setenv("VELA_NO_KEYCHAIN", "")
	// 钥匙串与文件都有：钥匙串优先，文件原样保留
	if s, src, _ := loadOrCreateSecret(dir); s != "钥匙串密钥" || src != SecretFromKeychain || !exists(filepath.Join(dir, "mail.key")) {
		t.Fatalf("钥匙串优先且不动文件：%q %v", s, src)
	}
}
