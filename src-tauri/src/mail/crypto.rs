//! 邮箱授权码的本地加密存储。
//!
//! 设计要点：
//! - 算法 AES-256-GCM（带认证的对称加密，既能保密也能防篡改）
//! - 密钥不落盘到数据库，而是从 `VELA_SECRET` 环境变量或本机配置文件派生
//! - 每次加密都用新的随机 12 字节 nonce，密文格式为 `base64(nonce ‖ ciphertext ‖ tag)`
//! - 数据库里只有密文；即使数据库文件被拿走，没有密钥也无法还原授权码
//!
//! 密钥来源优先级：
//! 1. 环境变量 `VELA_SECRET`
//! 2. 应用配置目录下 `mail.key`（首次运行自动生成随机密钥，权限 0600）
//!
//! 注意：换了密钥后旧密文无法解密，需要用户重新输入邮箱授权码。

use std::path::{Path, PathBuf};

use aes_gcm::aead::{Aead, KeyInit, OsRng};
use aes_gcm::{Aes256Gcm, Nonce};
use base64::engine::general_purpose::STANDARD as B64;
use base64::Engine;
use rand::RngCore;
use sha2::{Digest, Sha256};

const NONCE_LEN: usize = 12;

/// 加密失败原因（面向用户的提示）
pub type CryptResult<T> = Result<T, String>;

/// 持有 32 字节密钥的加解密器
pub struct Crypto {
    cipher: Aes256Gcm,
}

impl Crypto {
    /// 从原始密钥材料（任意长度字符串）构造，内部用 SHA-256 压成 32 字节
    pub fn from_secret(secret: &str) -> Self {
        let key = Sha256::digest(secret.as_bytes());
        Self {
            cipher: Aes256Gcm::new_from_slice(&key).expect("SHA-256 输出恒为 32 字节"),
        }
    }

    /// 加密明文，返回 `base64(nonce ‖ ciphertext ‖ tag)`
    pub fn encrypt(&self, plain: &str) -> CryptResult<String> {
        let mut nonce_bytes = [0u8; NONCE_LEN];
        OsRng.fill_bytes(&mut nonce_bytes);
        let nonce = Nonce::from_slice(&nonce_bytes);

        let ciphertext = self
            .cipher
            .encrypt(nonce, plain.as_bytes())
            .map_err(|_| "加密失败".to_string())?;

        let mut out = Vec::with_capacity(NONCE_LEN + ciphertext.len());
        out.extend_from_slice(&nonce_bytes);
        out.extend_from_slice(&ciphertext);
        Ok(B64.encode(out))
    }

    /// 解密 `encrypt` 的输出
    pub fn decrypt(&self, encoded: &str) -> CryptResult<String> {
        let raw = B64
            .decode(encoded.trim())
            .map_err(|_| "密文格式损坏（无法解码）".to_string())?;
        if raw.len() <= NONCE_LEN {
            return Err("密文长度不足".into());
        }
        let (nonce_bytes, ciphertext) = raw.split_at(NONCE_LEN);
        let nonce = Nonce::from_slice(nonce_bytes);

        let plain = self
            .cipher
            .decrypt(nonce, ciphertext)
            .map_err(|_| "无法解密（密钥已变更或数据被篡改），请重新输入邮箱授权码".to_string())?;

        String::from_utf8(plain).map_err(|_| "解密结果不是合法文本".to_string())
    }
}

/// 解析密钥材料。找不到任何来源时返回 `None`，调用方应提示用户配置。
pub fn load_secret(app_config_dir: Option<&Path>) -> Option<String> {
    if let Ok(v) = std::env::var("VELA_SECRET") {
        let v = v.trim().to_string();
        if !v.is_empty() {
            return Some(v);
        }
    }

    // 尝试读取已有的 key 文件（不自动创建，避免只读目录下报错）
    if let Some(dir) = app_config_dir {
        let path = dir.join("mail.key");
        if let Ok(txt) = std::fs::read_to_string(&path) {
            let txt = txt.trim().to_string();
            if !txt.is_empty() {
                return Some(txt);
            }
        }
    }
    None
}

/// 生成并写入一个新的随机密钥文件，返回密钥内容。
///
/// 用于首次运行自动初始化；写入失败时返回错误，由调用方决定是否降级。
pub fn generate_secret_file(dir: &Path) -> CryptResult<String> {
    std::fs::create_dir_all(dir).map_err(|e| format!("无法创建配置目录：{e}"))?;
    let path: PathBuf = dir.join("mail.key");

    let mut buf = [0u8; 32];
    OsRng.fill_bytes(&mut buf);
    let secret = B64.encode(buf);

    std::fs::write(&path, &secret).map_err(|e| format!("无法写入密钥文件：{e}"))?;
    restrict_permissions(&path);
    Ok(secret)
}

/// 尽力收紧文件权限（Windows 上 chmod 语义有限，失败不算致命）
#[cfg(unix)]
fn restrict_permissions(path: &Path) {
    use std::os::unix::fs::PermissionsExt;
    let _ = std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600));
}

#[cfg(not(unix))]
fn restrict_permissions(_path: &Path) {
    // Windows：文件位于用户配置目录，默认已仅当前用户可读
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn roundtrip() {
        let c = Crypto::from_secret("test-secret");
        let enc = c.encrypt("my-auth-code").unwrap();
        assert_ne!(enc, "my-auth-code");
        assert_eq!(c.decrypt(&enc).unwrap(), "my-auth-code");
    }

    #[test]
    fn nonce_is_random_per_call() {
        let c = Crypto::from_secret("s");
        let a = c.encrypt("same").unwrap();
        let b = c.encrypt("same").unwrap();
        assert_ne!(a, b, "相同明文两次加密必须产生不同密文");
        assert_eq!(c.decrypt(&a).unwrap(), c.decrypt(&b).unwrap());
    }

    #[test]
    fn wrong_secret_fails() {
        let enc = Crypto::from_secret("k1").encrypt("secret").unwrap();
        assert!(Crypto::from_secret("k2").decrypt(&enc).is_err());
    }

    #[test]
    fn tampered_ciphertext_fails() {
        let c = Crypto::from_secret("k");
        let enc = c.encrypt("secret").unwrap();
        let mut raw = B64.decode(&enc).unwrap();
        let last = raw.len() - 1;
        raw[last] ^= 0xFF; // 翻转认证标签的一个比特
        assert!(c.decrypt(&B64.encode(raw)).is_err(), "GCM 必须检测出篡改");
    }

    #[test]
    fn garbage_input_fails_gracefully() {
        let c = Crypto::from_secret("k");
        assert!(c.decrypt("不是 base64!!").is_err());
        assert!(c.decrypt("").is_err());
        assert!(c.decrypt(&B64.encode([0u8; 5])).is_err());
    }

    #[test]
    fn unicode_and_long_secrets() {
        let c = Crypto::from_secret("密钥🔑");
        let plain = "授权码 with unicode ☕ and spaces";
        assert_eq!(c.decrypt(&c.encrypt(plain).unwrap()).unwrap(), plain);
    }
}
