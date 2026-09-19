//! 本地单文件数据库（SQLite）。
//!
//! 架构说明：应用是纯本地单用户形态——邮件、账户凭证、AI 分析结果全部
//! 存在应用数据目录的一个 SQLite 文件里，没有服务端、没有账号体系。
//! `mail.key`（AES-256-GCM）继续负责邮箱授权码的加解密。
//!
//! 邮件行的 JSON 形状与前端 `ServerEmail` 约定一致（camelCase），
//! 序列化/解析集中在这里，命令层只做转发。

use std::path::Path;
use std::sync::{Arc, Mutex};

use rusqlite::Connection;
use serde_json::Value;

/// 全局数据库句柄（Tauri managed state）。SQLite 连接非线程安全，用 Mutex 串行化；
/// Arc 包装使命令可以把它克隆进阻塞线程池。
#[derive(Clone)]
pub struct Db(pub Arc<Mutex<Connection>>);

/// 递归剔除 JSON 字符串里的 NUL 字节：SQLite/PG 的 TEXT 都容不下 \0，
/// 混进来会让整批写入失败。在唯一写入口统一清洗。
pub fn strip_nul(v: &mut Value) {
    match v {
        Value::String(s) => {
            if s.contains('\0') {
                *s = s.replace('\0', "");
            }
        }
        Value::Array(a) => a.iter_mut().for_each(strip_nul),
        Value::Object(o) => o.values_mut().for_each(strip_nul),
        _ => {}
    }
}

/// 打开（必要时创建）数据库文件并建表。
pub fn open(path: &Path) -> Result<Connection, String> {
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir).map_err(|e| format!("无法创建数据目录：{e}"))?;
    }
    let conn = Connection::open(path).map_err(|e| format!("无法打开本地数据库：{e}"))?;
    conn.pragma_update(None, "journal_mode", "WAL")
        .map_err(|e| format!("设置 WAL 失败：{e}"))?;
    conn.execute_batch(SCHEMA)
        .map_err(|e| format!("初始化本地数据库失败：{e}"))?;
    Ok(conn)
}

pub(crate) const SCHEMA: &str = r#"
CREATE TABLE IF NOT EXISTS emails (
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL,
  folder       TEXT NOT NULL,
  sender       TEXT NOT NULL,
  recipients   TEXT NOT NULL,
  cc           TEXT,
  subject      TEXT NOT NULL,
  body         TEXT NOT NULL,
  html_body    TEXT,
  date         TEXT NOT NULL,
  read         INTEGER NOT NULL DEFAULT 0,
  starred      INTEGER NOT NULL DEFAULT 0,
  category     TEXT,
  ai_summary   TEXT,
  ai_why       TEXT,
  attachments  TEXT NOT NULL DEFAULT '[]',
  thread_count INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_emails_date ON emails(date);

CREATE TABLE IF NOT EXISTS mailbox_accounts (
  id            TEXT PRIMARY KEY,
  email         TEXT NOT NULL,
  provider      TEXT NOT NULL DEFAULT 'custom',
  label         TEXT NOT NULL DEFAULT '',
  imap_host     TEXT NOT NULL,
  imap_port     INTEGER NOT NULL,
  imap_security TEXT NOT NULL DEFAULT 'ssl',
  smtp_host     TEXT NOT NULL,
  smtp_port     INTEGER NOT NULL,
  smtp_security TEXT NOT NULL DEFAULT 'ssl',
  secret_enc    TEXT NOT NULL,
  cursors       TEXT NOT NULL DEFAULT '[]'
);
"#;

/* ———————————————— 邮件 ———————————————— */

/// 读取全部邮件（按时间倒序），形状对齐前端 `ServerEmail`。
pub fn get_emails(conn: &Connection) -> Result<Vec<Value>, String> {
    let mut stmt = conn
        .prepare("SELECT id, account_id, folder, sender, recipients, cc, subject, body,
                         html_body, date, read, starred, category, ai_summary, ai_why,
                         attachments, thread_count
                  FROM emails ORDER BY date DESC")
        .map_err(|e| e.to_string())?;
    let rows = stmt
        .query_map([], |r| {
            Ok((
                r.get::<_, String>(0)?,
                r.get::<_, String>(1)?,
                r.get::<_, String>(2)?,
                r.get::<_, String>(3)?,
                r.get::<_, String>(4)?,
                r.get::<_, Option<String>>(5)?,
                r.get::<_, String>(6)?,
                r.get::<_, String>(7)?,
                r.get::<_, Option<String>>(8)?,
                r.get::<_, String>(9)?,
                r.get::<_, i64>(10)? != 0,
                r.get::<_, i64>(11)? != 0,
                r.get::<_, Option<String>>(12)?,
                r.get::<_, Option<String>>(13)?,
                r.get::<_, Option<String>>(14)?,
                r.get::<_, String>(15)?,
                r.get::<_, i64>(16)?,
            ))
        })
        .map_err(|e| e.to_string())?;

    let mut out = Vec::new();
    for row in rows {
        let (id, account_id, folder, sender, recipients, cc, subject, body, html_body, date, read,
             starred, category, ai_summary, ai_why, attachments, thread_count) =
            row.map_err(|e| e.to_string())?;
        out.push(serde_json::json!({
            "id": id,
            "accountId": account_id,
            "folder": folder,
            "from": serde_json::from_str::<Value>(&sender).unwrap_or(Value::Null),
            "to": serde_json::from_str::<Value>(&recipients).unwrap_or(Value::Null),
            "cc": cc.and_then(|c| serde_json::from_str::<Value>(&c).ok()),
            "subject": subject,
            "body": body,
            "htmlBody": html_body,
            "date": date,
            "read": read,
            "starred": starred,
            "category": category,
            "aiSummary": ai_summary,
            "aiWhy": ai_why,
            "attachments": serde_json::from_str::<Value>(&attachments).unwrap_or(Value::Null),
            "threadCount": thread_count,
        }));
    }
    Ok(out)
}

/// 全量 upsert（前端防抖同步：以本地内存状态为准）。
pub fn upsert_emails(conn: &Connection, emails: &[Value]) -> Result<(), String> {
    let mut stmt = conn
        .prepare(
            "INSERT OR REPLACE INTO emails
             (id, account_id, folder, sender, recipients, cc, subject, body, html_body,
              date, read, starred, category, ai_summary, ai_why, attachments, thread_count)
             VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15,?16,?17)",
        )
        .map_err(|e| e.to_string())?;
    for e in emails {
        let mut e = e.clone();
        strip_nul(&mut e); // SQLite TEXT 与 PG 一样容不下 \0
        let s = |k: &str| -> String {
            e.get(k)
                .map(|v| match v {
                    Value::String(s) => s.clone(),
                    other => other.to_string(),
                })
                .unwrap_or_default()
        };
        let opt = |k: &str| -> Option<String> {
            e.get(k).and_then(|v| match v {
                Value::Null => None,
                Value::String(s) => Some(s.clone()),
                other => Some(other.to_string()),
            })
        };
        let json_at = |k: &str| -> String {
            e.get(k)
                .filter(|v| !v.is_null())
                .map(|v| v.to_string())
                .unwrap_or_else(|| "null".into())
        };
        stmt.execute(rusqlite::params![
            s("id"),
            s("accountId"),
            s("folder"),
            json_at("from"),
            json_at("to"),
            opt("cc"),
            s("subject"),
            s("body"),
            opt("htmlBody"),
            s("date"),
            bool_of(&e, "read"),
            bool_of(&e, "starred"),
            opt("category"),
            opt("aiSummary"),
            opt("aiWhy"),
            json_at("attachments"),
            e.get("threadCount").and_then(|v| v.as_i64()).unwrap_or(1),
        ])
        .map_err(|err| format!("写入邮件失败：{err}"))?;
    }
    Ok(())
}

fn bool_of(e: &Value, k: &str) -> i64 {
    u64::from(e.get(k).and_then(|v| v.as_bool()).unwrap_or(false)) as i64
}

/// 删除指定邮件（对账剔除、清空垃圾箱、移除账户时调用）。
pub fn delete_emails(conn: &Connection, ids: &[String]) -> Result<(), String> {
    let mut stmt = conn
        .prepare("DELETE FROM emails WHERE id = ?1")
        .map_err(|e| e.to_string())?;
    for id in ids {
        stmt.execute([id]).map_err(|e| e.to_string())?;
    }
    Ok(())
}

/// 删除某账户的全部邮件（移除邮箱账户时）。
pub fn delete_account_emails(conn: &Connection, account_id: &str) -> Result<(), String> {
    conn.execute("DELETE FROM emails WHERE account_id = ?1", [account_id])
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn mem_db() -> Connection {
        let conn = Connection::open_in_memory().unwrap();
        conn.execute_batch(SCHEMA).unwrap();
        conn
    }

    #[test]
    fn upsert_get_roundtrip() {
        let conn = mem_db();
        let mail = serde_json::json!({
            "id": "a@b.com:inbox:1", "accountId": "a@b.com", "folder": "inbox",
            "from": {"name": "张三", "email": "z@x.com"},
            "to": [{"name": "我", "email": "a@b.com"}],
            "cc": null, "subject": "主题", "body": "正文\0带nul",
            "htmlBody": null, "date": "2026-09-19T10:00:00+08:00",
            "read": false, "starred": true, "category": null,
            "aiSummary": null, "aiWhy": null,
            "attachments": [], "threadCount": 1, "real": true,
        });
        upsert_emails(&conn, &[mail]).unwrap();
        let out = get_emails(&conn).unwrap();
        assert_eq!(out.len(), 1);
        assert_eq!(out[0]["subject"], "主题");
        assert_eq!(out[0]["body"], "正文带nul"); // NUL 已清洗
        assert_eq!(out[0]["from"]["email"], "z@x.com");
        assert!(out[0]["starred"].as_bool().unwrap());
        assert!(out[0]["cc"].is_null());
    }

    #[test]
    fn delete_emails_works() {
        let conn = mem_db();
        let mail = serde_json::json!({
            "id": "x", "accountId": "a", "folder": "inbox",
            "from": {"name": "", "email": "z@x.com"}, "to": [],
            "subject": "s", "body": "b", "date": "2026-01-01T00:00:00Z",
        });
        upsert_emails(&conn, &[mail]).unwrap();
        delete_emails(&conn, &["x".into()]).unwrap();
        assert!(get_emails(&conn).unwrap().is_empty());
    }
}
