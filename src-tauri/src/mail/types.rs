//! 邮件协议层与前端之间的数据契约。
//!
//! 这些结构会经由 Tauri command 序列化成 JSON 传给前端，
//! 因此字段名统一使用 camelCase，与 `src/types.ts` 的 `Email` 对齐。

use serde::{Deserialize, Serialize};

/// 服务端文件夹归属（前端 FolderId 的子集）
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ServerFolder {
    Inbox,
    Sent,
}

/// 增量同步游标。
///
/// 两个游标各管一个方向，才能既同步新邮件、又完整拉回历史邮件：
///
/// - `last_uid`（向上）：已同步到的最新 UID。下次只拉 `uid > last_uid`。
/// - `backfill_uid`（向下）：尚未拉取的更旧邮件的最大 UID。
///   * `None`：尚未开始回填（首次同步前）
///   * `Some(0)`：历史邮件已全部拉完
///   * `Some(n)`：还需拉取 `uid <= n` 的部分
///
/// 为什么需要 backfill：一个邮箱可能有上万封历史邮件，不可能一次全下完。
/// 若只靠 `last_uid` 单游标，首次同步截断后游标已跳到最末尾，
/// 被丢掉的老邮件就再也没有机会拉取（早期实现就有这个缺陷）。
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MailboxCursor {
    pub folder: ServerFolder,
    /// 已同步到的最新 UID，下次只拉 uid > last_uid 的邮件
    pub last_uid: u32,
    /// 历史邮件回填进度；`None` = 尚未开始，`Some(0)` = 已完成
    #[serde(default)]
    pub backfill_uid: Option<u32>,
    /// 服务器返回的 UIDVALIDITY。该值变化说明邮箱被重建，旧 UID 全部作废，需重新全量同步
    pub uid_validity: u32,
    /// 服务端真实文件夹名（如 QQ 的 "Sent Messages"），回写状态时要用
    pub remote_name: String,
}

impl MailboxCursor {
    /// 是否还有更早的历史邮件待拉取
    pub fn has_pending_history(&self) -> bool {
        matches!(self.backfill_uid, Some(n) if n > 0)
    }
}

impl From<super::providers::FolderKind> for ServerFolder {
    fn from(k: super::providers::FolderKind) -> Self {
        use super::providers::FolderKind;
        match k {
            FolderKind::Sent => ServerFolder::Sent,
            FolderKind::Archive | FolderKind::Trash | FolderKind::Drafts => ServerFolder::Inbox,
        }
    }
}

/// 邮件地址
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MailAddress {
    pub name: String,
    pub email: String,
}

/// 附件元信息（只保留名称、大小、类型，不下载内容）
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct FetchedAttachment {
    pub name: String,
    pub size_kb: u32,
    /// pdf / sheet / doc / image / zip / code，对应前端图标
    pub kind: String,
}

/// 从邮箱服务器拉下来的一封邮件
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct FetchedMail {
    /// IMAP UID，在同一个文件夹内唯一且稳定；前端用它回写已读/星标
    pub uid: u32,
    /// 所属邮箱账户（用邮箱地址作为账户 id）
    pub account_id: String,
    /// 该邮件所在的服务端文件夹
    pub server_folder: ServerFolder,
    /// 服务端文件夹的原始名称（如 "INBOX"、"Sent Messages"），回写状态时必须原样传回
    pub remote_name: String,
    /// RFC5322 Message-ID，用于跨账户去重
    pub message_id: Option<String>,
    pub subject: String,
    pub from_name: String,
    pub from_email: String,
    pub to: Vec<MailAddress>,
    pub cc: Vec<MailAddress>,
    /// 纯文本正文
    pub body: String,
    /// RFC3339 时间
    pub date: String,
    pub read: bool,
    pub starred: bool,
    pub attachments: Vec<FetchedAttachment>,
}

impl FetchedMail {
    /// 转成前端 `Email` 结构所需的 JSON。
    ///
    /// 前端 id 用 `账户:文件夹:UID` 组合，保证跨账户、跨文件夹都不冲突且稳定
    /// （稳定的 id 才能让「浏览器本地补丁」和数据库 upsert 正确对应同一封邮件）。
    pub fn to_email_json(&self) -> serde_json::Value {
        let folder = match self.server_folder {
            ServerFolder::Inbox => "inbox",
            ServerFolder::Sent => "sent",
        };
        let id = format!("{}:{}:{}", self.account_id, folder, self.uid);

        let contacts = |list: &[MailAddress]| -> Vec<serde_json::Value> {
            list.iter()
                .map(|a| {
                    serde_json::json!({
                        "name": if a.name.is_empty() { a.email.clone() } else { a.name.clone() },
                        "email": a.email,
                    })
                })
                .collect()
        };

        let attachments: Vec<serde_json::Value> = self
            .attachments
            .iter()
            .enumerate()
            .map(|(i, a)| {
                serde_json::json!({
                    "id": format!("{id}:attachment:{i}"),
                    "name": a.name,
                    "sizeKb": a.size_kb,
                    "kind": a.kind,
                })
            })
            .collect();

        serde_json::json!({
            "id": id,
            "accountId": self.account_id,
            "folder": folder,
            "from": {
                "name": if self.from_name.is_empty() { self.from_email.clone() } else { self.from_name.clone() },
                "email": self.from_email,
            },
            "to": contacts(&self.to),
            "cc": if self.cc.is_empty() { serde_json::Value::Null } else { serde_json::Value::Array(contacts(&self.cc)) },
            "subject": self.subject,
            "body": self.body,
            "date": self.date,
            "read": self.read,
            "starred": self.starred,
            "category": serde_json::Value::Null,
            "aiSummary": serde_json::Value::Null,
            "aiWhy": serde_json::Value::Null,
            "attachments": attachments,
            "threadCount": 1,
            // 下面是真实账户专属的元数据，前端据此回写服务器状态
            "uid": self.uid,
            "serverFolder": folder,
            "remoteName": self.remote_name,
            "messageId": self.message_id,
            "real": true,
        })
    }
}

/// 回写服务器状态时要定位的一封邮件
#[allow(dead_code)] // 供后续「在服务器上定位邮件」的功能直接使用
#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RemoteRef {
    /// 邮箱账户 id（= 邮箱地址）
    pub account_id: String,
    pub uid: u32,
    /// 该邮件当前所在的服务端文件夹名（如 "INBOX"、"Sent Messages"）
    pub remote_name: String,
}

/// 邮箱账户（不含密码），用于前端展示
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MailboxAccount {
    pub id: String,
    pub email: String,
    pub provider: String,
    pub label: String,
    pub imap_host: String,
    pub imap_port: u16,
    pub smtp_host: String,
    pub smtp_port: u16,
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample() -> FetchedMail {
        FetchedMail {
            uid: 42,
            account_id: "me@qq.com".into(),
            server_folder: ServerFolder::Inbox,
            remote_name: "INBOX".into(),
            message_id: Some("<abc@qq.com>".into()),
            subject: "测试主题".into(),
            from_name: "张三".into(),
            from_email: "zhangsan@example.com".into(),
            to: vec![MailAddress {
                name: "我".into(),
                email: "me@qq.com".into(),
            }],
            cc: vec![],
            body: "正文".into(),
            date: "2026-09-18T10:00:00+08:00".into(),
            read: false,
            starred: true,
            attachments: vec![FetchedAttachment {
                name: "a.pdf".into(),
                size_kb: 12,
                kind: "pdf".into(),
            }],
        }
    }

    #[test]
    fn email_id_is_stable_and_unique() {
        let e = sample();
        let j = e.to_email_json();
        assert_eq!(j["id"], "me@qq.com:inbox:42");
        // 同一封邮件反复转换必须得到同样的 id
        assert_eq!(j["id"], e.to_email_json()["id"]);
    }

    #[test]
    fn sent_folder_maps_to_sent() {
        let mut e = sample();
        e.server_folder = ServerFolder::Sent;
        assert_eq!(e.to_email_json()["folder"], "sent");
    }

    #[test]
    fn empty_name_falls_back_to_email() {
        let mut e = sample();
        e.from_name = String::new();
        let j = e.to_email_json();
        assert_eq!(j["from"]["name"], "zhangsan@example.com");
    }

    #[test]
    fn empty_cc_becomes_null() {
        let j = sample().to_email_json();
        assert!(j["cc"].is_null(), "无抄送时前端期望 null 而非空数组");
    }

    #[test]
    fn attachment_ids_are_unique() {
        let mut e = sample();
        e.attachments = vec![
            FetchedAttachment {
                name: "a.pdf".into(),
                size_kb: 1,
                kind: "pdf".into(),
            },
            FetchedAttachment {
                name: "b.pdf".into(),
                size_kb: 2,
                kind: "pdf".into(),
            },
        ];
        let j = e.to_email_json();
        assert_ne!(j["attachments"][0]["id"], j["attachments"][1]["id"]);
    }

    #[test]
    fn real_flag_marked() {
        assert_eq!(sample().to_email_json()["real"], true);
    }

    #[test]
    fn remote_name_exposed_for_flag_writeback() {
        // 回写已读/星标必须知道服务器上的原始文件夹名
        assert_eq!(sample().to_email_json()["remoteName"], "INBOX");
    }
}
