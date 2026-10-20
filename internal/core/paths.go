package core

import (
	"os"
	"path/filepath"
	"runtime"
)

// Identifier 是原 Tauri 版的 bundle identifier。数据目录沿用它，
// 这样从 Tauri 版升级的用户能直接读到原来的 vela.db 和 mail.key。
const Identifier = "app.vela.mail"

// DataDir 返回 Tauri app_data_dir 的同一位置：
// Linux $XDG_DATA_HOME/app.vela.mail（默认 ~/.local/share/...），
// macOS ~/Library/Application Support/app.vela.mail，Windows %APPDATA%\app.vela.mail。
func DataDir() string {
	if v := os.Getenv("VELA_DATA_DIR"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, Identifier)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", Identifier)
	default:
		d, _ := os.UserConfigDir()
		return filepath.Join(d, Identifier)
	}
}

// ConfigDir 返回 Tauri app_config_dir 的同一位置（mail.key 所在）。
func ConfigDir() string {
	if v := os.Getenv("VELA_DATA_DIR"); v != "" {
		return v
	}
	d, _ := os.UserConfigDir()
	return filepath.Join(d, Identifier)
}
