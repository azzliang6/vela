//! 邮箱账户与凭证的数据库存取。
//!
//! 表结构：
//! ```sql
//! CREATE TABLE mailbox_accounts (
//!   user_id     INT  NOT NULL,           -- 归属用户
//!   id          TEXT NOT NULL,           -- 账户 id（用邮箱地址）
//!   email       TEXT NOT NULL,
//!   provider    TEXT NOT NULL,
//!   label       TEXT NOT NULL,
//!   imap_host   TEXT NOT NULL,
//!   imap_port   INT  NOT NULL,
//!   imap_security TEXT NOT NULL,
//!   smtp_host   TEXT NOT NULL,
//!   smtp_port   INT  NOT NULL,
//!   smtp_security TEXT NOT NULL,
//!   secret_enc  TEXT NOT NULL,           -- AES-256-GCM 加密后的授权码
//!   cursors     JSONB NOT NULL,          -- 各文件夹的 UID 游标
//!   created_at  TIMESTAMPTZ,
//!   PRIMARY KEY (user_id, id)
//! );
//! ```
//!
//! 安全约定：授权码明文只在内存中短暂存在；落库前必加密，读出后立即用于建立连接。

use serde_json::Value;
use tokio_postgres::Client;

use super::crypto::Crypto;
use super::providers::{apply_preset, MailServerConfig, Security};
use super::types::{MailboxAccount, MailboxCursor};

/// 建表语句（幂等）
pub const SCHEMA: &str = r#"
CREATE TABLE IF NOT EXISTS mailbox_accounts (
  user_id       INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  id            TEXT NOT NULL,
  email         TEXT NOT NULL,
  provider      TEXT NOT NULL DEFAULT 'custom',
  label         TEXT NOT NULL DEFAULT '',
  imap_host     TEXT NOT NULL,
  imap_port     INT  NOT NULL,
  imap_security TEXT NOT NULL DEFAULT 'ssl',
  smtp_host     TEXT NOT NULL,
  smtp_port     INT  NOT NULL,
  smtp_security TEXT NOT NULL DEFAULT 'ssl',
  secret_enc    TEXT NOT NULL,
  cursors       JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, id)
);
"#;

/// 前后端传输用的加密方式字符串
fn security_to_str(s: Security) -> &'static str {
    match s {
        Security::Ssl => "ssl",
        Security::Starttls => "starttls",
        Security::None => "none",
    }
}

fn security_from_str(s: &str) -> Security {
    match s {
        "starttls" => Security::Starttls,
        "none" => Security::None,
        _ => Security::Ssl,
    }
}

/// 新增或更新一个邮箱账户（凭证加密存储）
pub async fn upsert_account(
    client: &Client,
    user_id: i32,
    cfg: &MailServerConfig,
    label: &str,
    crypto: &Crypto,
) -> Result<MailboxAccount, String> {
    let secret_enc = crypto.encrypt(&cfg.password)?;
    let account_id = cfg.email.clone();

    client
        .execute(
            r#"
            INSERT INTO mailbox_accounts (
              user_id, id, email, provider, label,
              imap_host, imap_port, imap_security,
              smtp_host, smtp_port, smtp_security, secret_enc
            ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
            ON CONFLICT (user_id, id) DO UPDATE SET
              provider = EXCLUDED.provider, label = EXCLUDED.label,
              imap_host = EXCLUDED.imap_host, imap_port = EXCLUDED.imap_port,
              imap_security = EXCLUDED.imap_security,
              smtp_host = EXCLUDED.smtp_host, smtp_port = EXCLUDED.smtp_port,
              smtp_security = EXCLUDED.smtp_security,
              secret_enc = EXCLUDED.secret_enc
            "#,
            &[
                &user_id,
                &account_id,
                &cfg.email,
                &cfg.provider,
                &label,
                &cfg.imap_host,
                &(cfg.imap_port as i32),
                &security_to_str(cfg.imap_security),
                &cfg.smtp_host,
                &(cfg.smtp_port as i32),
                &security_to_str(cfg.smtp_security),
                &secret_enc,
            ],
        )
        .await
        .map_err(|e| format!("保存邮箱账户失败：{e}"))?;

    Ok(MailboxAccount {
        id: account_id,
        email: cfg.email.clone(),
        provider: cfg.provider.clone(),
        label: label.to_string(),
        imap_host: cfg.imap_host.clone(),
        imap_port: cfg.imap_port,
        smtp_host: cfg.smtp_host.clone(),
        smtp_port: cfg.smtp_port,
    })
}

/// 列出用户的所有邮箱账户（不含凭证）
pub async fn list_accounts(client: &Client, user_id: i32) -> Result<Vec<MailboxAccount>, String> {
    let rows = client
        .query(
            "SELECT id, email, provider, label, imap_host, imap_port, smtp_host, smtp_port
             FROM mailbox_accounts WHERE user_id = $1 ORDER BY created_at",
            &[&user_id],
        )
        .await
        .map_err(|e| format!("读取邮箱账户失败：{e}"))?;

    Ok(rows
        .iter()
        .map(|r| MailboxAccount {
            id: r.get(0),
            email: r.get(1),
            provider: r.get(2),
            label: r.get(3),
            imap_host: r.get(4),
            imap_port: r.get::<_, i32>(5) as u16,
            smtp_host: r.get(6),
            smtp_port: r.get::<_, i32>(7) as u16,
        })
        .collect())
}

/// 取回完整连接配置（含解密后的授权码），用于建立 IMAP/SMTP 连接
pub async fn load_config(
    client: &Client,
    user_id: i32,
    account_id: &str,
    crypto: &Crypto,
) -> Result<MailServerConfig, String> {
    let row = client
        .query_opt(
            "SELECT email, provider, imap_host, imap_port, imap_security,
                    smtp_host, smtp_port, smtp_security, secret_enc
             FROM mailbox_accounts WHERE user_id = $1 AND id = $2",
            &[&user_id, &account_id],
        )
        .await
        .map_err(|e| format!("读取邮箱账户失败：{e}"))?
        .ok_or_else(|| "邮箱账户不存在".to_string())?;

    let password = crypto.decrypt(&row.get::<_, String>(8))?;

    let mut cfg = MailServerConfig {
        provider: row.get(1),
        email: row.get(0),
        imap_host: row.get(2),
        imap_port: row.get::<_, i32>(3) as u16,
        imap_security: security_from_str(&row.get::<_, String>(4)),
        smtp_host: row.get(5),
        smtp_port: row.get::<_, i32>(6) as u16,
        smtp_security: security_from_str(&row.get::<_, String>(7)),
        password,
    };

    // 自定义服务商的服务器地址以数据库为准；预设服务商允许被库中值覆盖后重新校验
    if cfg.provider == "custom" {
        if cfg.imap_host.trim().is_empty() || cfg.smtp_host.trim().is_empty() {
            return Err("该自定义账户缺少服务器地址，请重新添加".into());
        }
    } else {
        apply_preset(&mut cfg).ok(); // 补齐端口/加密方式，失败也不阻断（保持库中值）
    }

    Ok(cfg)
}

/// 删除账户（级联删除其邮件由调用方处理）
pub async fn delete_account(
    client: &Client,
    user_id: i32,
    account_id: &str,
) -> Result<(), String> {
    client
        .execute(
            "DELETE FROM mailbox_accounts WHERE user_id = $1 AND id = $2",
            &[&user_id, &account_id],
        )
        .await
        .map_err(|e| format!("删除邮箱账户失败：{e}"))?;
    Ok(())
}

/// 读取某账户的同步游标
pub async fn load_cursors(
    client: &Client,
    user_id: i32,
    account_id: &str,
) -> Result<Vec<MailboxCursor>, String> {
    let row = client
        .query_opt(
            "SELECT cursors FROM mailbox_accounts WHERE user_id = $1 AND id = $2",
            &[&user_id, &account_id],
        )
        .await
        .map_err(|e| format!("读取同步游标失败：{e}"))?;

    let Some(r) = row else {
        return Ok(Vec::new());
    };
    let v: Value = r.get(0);
    // 游标损坏时按「首次同步」处理，而不是让整个同步失败
    Ok(serde_json::from_value(v).unwrap_or_default())
}

/// 写回同步游标
pub async fn save_cursors(
    client: &Client,
    user_id: i32,
    account_id: &str,
    cursors: &[MailboxCursor],
) -> Result<(), String> {
    let v = serde_json::to_value(cursors).map_err(|e| format!("序列化游标失败：{e}"))?;
    client
        .execute(
            "UPDATE mailbox_accounts SET cursors = $3 WHERE user_id = $1 AND id = $2",
            &[&user_id, &account_id, &v],
        )
        .await
        .map_err(|e| format!("保存同步游标失败：{e}"))?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn security_roundtrip() {
        for s in [Security::Ssl, Security::Starttls, Security::None] {
            assert_eq!(security_from_str(security_to_str(s)), s);
        }
    }

    #[test]
    fn unknown_security_defaults_to_ssl() {
        // 保守默认：宁可多一层 TLS 也不要明文
        assert_eq!(security_from_str("garbage"), Security::Ssl);
        assert_eq!(security_from_str(""), Security::Ssl);
    }

    #[test]
    fn schema_creates_expected_table() {
        assert!(SCHEMA.contains("CREATE TABLE IF NOT EXISTS mailbox_accounts"));
        assert!(SCHEMA.contains("secret_enc"));
        assert!(
            SCHEMA.contains("ON DELETE CASCADE"),
            "用户被删除时其邮箱账户应一并清除"
        );
    }
}
