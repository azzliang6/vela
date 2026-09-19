//! 邮箱账户与凭证的本地存取（SQLite）。
//!
//! 安全约定：授权码明文只在内存中短暂存在；落库前必加密（AES-256-GCM，
//! 密钥来自应用配置目录的 `mail.key`），读出后立即用于建立连接。
//! 表结构定义见 `crate::localdb::SCHEMA`（单用户，无 user_id）。

use rusqlite::Connection;
use serde_json::Value;

use super::crypto::Crypto;
use super::providers::{apply_preset, MailServerConfig, Security};
use super::types::{MailboxAccount, MailboxCursor};

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
pub fn upsert_account(
    conn: &Connection,
    cfg: &MailServerConfig,
    label: &str,
    crypto: &Crypto,
) -> Result<MailboxAccount, String> {
    let secret_enc = crypto.encrypt(&cfg.password)?;
    let account_id = cfg.email.clone();

    conn.execute(
        "INSERT INTO mailbox_accounts
         (id, email, provider, label, imap_host, imap_port, imap_security,
          smtp_host, smtp_port, smtp_security, secret_enc)
         VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11)
         ON CONFLICT(id) DO UPDATE SET
           provider=excluded.provider, label=excluded.label,
           imap_host=excluded.imap_host, imap_port=excluded.imap_port,
           imap_security=excluded.imap_security,
           smtp_host=excluded.smtp_host, smtp_port=excluded.smtp_port,
           smtp_security=excluded.smtp_security, secret_enc=excluded.secret_enc",
        rusqlite::params![
            account_id,
            cfg.email,
            cfg.provider,
            label,
            cfg.imap_host,
            cfg.imap_port,
            security_to_str(cfg.imap_security),
            cfg.smtp_host,
            cfg.smtp_port,
            security_to_str(cfg.smtp_security),
            secret_enc,
        ],
    )
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

/// 列出所有邮箱账户（不含凭证）
pub fn list_accounts(conn: &Connection) -> Result<Vec<MailboxAccount>, String> {
    let mut stmt = conn
        .prepare(
            "SELECT id, email, provider, label, imap_host, imap_port, smtp_host, smtp_port
             FROM mailbox_accounts ORDER BY rowid",
        )
        .map_err(|e| format!("读取邮箱账户失败：{e}"))?;
    let rows = stmt
        .query_map([], |r| {
            Ok(MailboxAccount {
                id: r.get(0)?,
                email: r.get(1)?,
                provider: r.get(2)?,
                label: r.get(3)?,
                imap_host: r.get(4)?,
                imap_port: r.get::<_, i64>(5)? as u16,
                smtp_host: r.get(6)?,
                smtp_port: r.get::<_, i64>(7)? as u16,
            })
        })
        .map_err(|e| format!("读取邮箱账户失败：{e}"))?;
    rows.collect::<Result<Vec<_>, _>>()
        .map_err(|e| format!("读取邮箱账户失败：{e}"))
}

/// 取回完整连接配置（含解密后的授权码），用于建立 IMAP/SMTP 连接
pub fn load_config(
    conn: &Connection,
    account_id: &str,
    crypto: &Crypto,
) -> Result<MailServerConfig, String> {
    let row = conn
        .query_row(
            "SELECT email, provider, imap_host, imap_port, imap_security,
                    smtp_host, smtp_port, smtp_security, secret_enc
             FROM mailbox_accounts WHERE id = ?1",
            [account_id],
            |r| {
                Ok((
                    r.get::<_, String>(0)?,
                    r.get::<_, String>(1)?,
                    r.get::<_, String>(2)?,
                    r.get::<_, i64>(3)?,
                    r.get::<_, String>(4)?,
                    r.get::<_, String>(5)?,
                    r.get::<_, i64>(6)?,
                    r.get::<_, String>(7)?,
                    r.get::<_, String>(8)?,
                ))
            },
        )
        .map_err(|e| match e {
            rusqlite::Error::QueryReturnedNoRows => "邮箱账户不存在".to_string(),
            other => format!("读取邮箱账户失败：{other}"),
        })?;

    let password = crypto.decrypt(&row.8)?;

    let mut cfg = MailServerConfig {
        provider: row.1,
        email: row.0,
        imap_host: row.2,
        imap_port: row.3 as u16,
        imap_security: security_from_str(&row.4),
        smtp_host: row.5,
        smtp_port: row.6 as u16,
        smtp_security: security_from_str(&row.7),
        password,
    };

    // 自定义服务商的服务器地址以库中值为准；预设服务商补齐端口/加密方式
    if cfg.provider == "custom" {
        if cfg.imap_host.trim().is_empty() || cfg.smtp_host.trim().is_empty() {
            return Err("该自定义账户缺少服务器地址，请重新添加".into());
        }
    } else {
        apply_preset(&mut cfg).ok();
    }

    Ok(cfg)
}

/// 删除账户（其邮件由调用方一并清理）
pub fn delete_account(conn: &Connection, account_id: &str) -> Result<(), String> {
    conn.execute("DELETE FROM mailbox_accounts WHERE id = ?1", [account_id])
        .map_err(|e| format!("删除邮箱账户失败：{e}"))?;
    Ok(())
}

/// 读取某账户的同步游标
pub fn load_cursors(conn: &Connection, account_id: &str) -> Result<Vec<MailboxCursor>, String> {
    let v: Option<String> = conn
        .query_row(
            "SELECT cursors FROM mailbox_accounts WHERE id = ?1",
            [account_id],
            |r| r.get(0),
        )
        .map(Some)
        .or_else(|e| match e {
            rusqlite::Error::QueryReturnedNoRows => Ok(None),
            other => Err(format!("读取同步游标失败：{other}")),
        })?;

    let Some(text) = v else {
        return Ok(Vec::new());
    };
    // 游标损坏时按「首次同步」处理，而不是让整个同步失败
    let parsed: Result<Vec<MailboxCursor>, _> = serde_json::from_str(&text);
    Ok(parsed.unwrap_or_default())
}

/// 写回同步游标
pub fn save_cursors(
    conn: &Connection,
    account_id: &str,
    cursors: &[MailboxCursor],
) -> Result<(), String> {
    let v: Value = serde_json::to_value(cursors).map_err(|e| format!("序列化游标失败：{e}"))?;
    conn.execute(
        "UPDATE mailbox_accounts SET cursors = ?2 WHERE id = ?1",
        rusqlite::params![account_id, v.to_string()],
    )
    .map_err(|e| format!("保存同步游标失败：{e}"))?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_conn() -> Connection {
        let conn = Connection::open_in_memory().unwrap();
        conn.execute_batch(crate::localdb::SCHEMA).unwrap();
        conn
    }

    fn cfg() -> MailServerConfig {
        MailServerConfig {
            provider: "qq".into(),
            email: "a@qq.com".into(),
            imap_host: "imap.qq.com".into(),
            imap_port: 993,
            imap_security: Security::Ssl,
            smtp_host: "smtp.qq.com".into(),
            smtp_port: 465,
            smtp_security: Security::Ssl,
            password: "secret".into(),
        }
    }

    #[test]
    fn account_roundtrip_with_cursors() {
        let conn = test_conn();
        let crypto = Crypto::from_secret("k");
        upsert_account(&conn, &cfg(), "我的QQ", &crypto).unwrap();

        let list = list_accounts(&conn).unwrap();
        assert_eq!(list.len(), 1);
        assert_eq!(list[0].label, "我的QQ");

        let loaded = load_config(&conn, "a@qq.com", &crypto).unwrap();
        assert_eq!(loaded.password, "secret"); // 解密回原明文
        assert_eq!(loaded.imap_host, "imap.qq.com");

        save_cursors(
            &conn,
            "a@qq.com",
            &[MailboxCursor {
                folder: crate::mail::types::ServerFolder::Inbox,
                last_uid: 42,
                backfill_uid: Some(7),
                uid_validity: 1,
                remote_name: "INBOX".into(),
            }],
        )
        .unwrap();
        let cursors = load_cursors(&conn, "a@qq.com").unwrap();
        assert_eq!(cursors[0].last_uid, 42);

        delete_account(&conn, "a@qq.com").unwrap();
        assert!(list_accounts(&conn).unwrap().is_empty());
        assert!(load_cursors(&conn, "a@qq.com").unwrap().is_empty());
    }
}
