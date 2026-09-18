//! 暴露给前端的真实邮箱操作（Tauri commands）。
//!
//! 前端的 `api.ts` 封装了两种后端：
//! - 浏览器环境：调用 `http://127.0.0.1:3210/api/mail/*`（Node 版 server/index.mjs）
//! - 桌面环境：通过 `invoke("...")` 调用本模块
//!
//! 因为 IMAP/SMTP 是同步阻塞 IO，这里用 `spawn_blocking` 把网络操作挪到阻塞线程池，
//! 避免卡住 Tauri 的异步运行时（否则界面会假死）。

use serde::Deserialize;

use super::imap;
use super::providers::{self, apply_preset, FolderKind, MailServerConfig, ProviderPreset};
use super::store;
use super::smtp::{self, OutgoingMail};
use crate::server;

/// 统一的命令错误类型：前端拿到的是字符串消息
type CmdResult<T> = Result<T, String>;

/// 从前端传入的邮箱账户添加参数
#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AddAccountInput {
    pub provider: String,
    pub email: String,
    pub password: String,
    #[serde(default)]
    pub label: String,
    /// 自定义服务商需要手填；预设服务商会被 apply_preset 覆盖
    #[serde(default)]
    pub imap_host: String,
    #[serde(default)]
    pub imap_port: u16,
    #[serde(default)]
    pub smtp_host: String,
    #[serde(default)]
    pub smtp_port: u16,
}

impl AddAccountInput {
    fn into_config(self) -> CmdResult<MailServerConfig> {
        let mut cfg = MailServerConfig {
            provider: self.provider.trim().to_string(),
            email: self.email.trim().to_string(),
            imap_host: self.imap_host.trim().to_string(),
            imap_port: if self.imap_port == 0 { 993 } else { self.imap_port },
            imap_security: providers::Security::Ssl,
            smtp_host: self.smtp_host.trim().to_string(),
            smtp_port: if self.smtp_port == 0 { 465 } else { self.smtp_port },
            smtp_security: providers::Security::Ssl,
            password: self.password,
        };
        apply_preset(&mut cfg)?;
        if cfg.password.trim().is_empty() {
            return Err("请填写邮箱授权码 / 密码".into());
        }
        Ok(cfg)
    }
}

/// 取得当前的加密器。
///
/// 密钥来源：`VELA_SECRET` 环境变量优先；否则用应用配置目录下的 `mail.key`
/// （不存在时自动生成一个随机密钥）。
fn crypto(app: &tauri::AppHandle) -> CmdResult<super::Crypto> {
    if let Some(secret) = super::crypto::load_secret(None) {
        return Ok(super::Crypto::from_secret(&secret));
    }

    let dir = tauri::Manager::path(app).app_config_dir().ok();
    if let Some(dir) = dir {
        if let Some(secret) = super::crypto::load_secret(Some(&dir)) {
            return Ok(super::Crypto::from_secret(&secret));
        }
        let secret = super::crypto::generate_secret_file(&dir)?;
        return Ok(super::Crypto::from_secret(&secret));
    }

    Err("无法确定密钥存储位置，请设置 VELA_SECRET 环境变量".into())
}

/* ———————————————— 命令 ———————————————— */

/// 服务商预设列表（含 IMAP/SMTP 地址与申请授权码的提示）
#[tauri::command]
pub fn mail_providers() -> Vec<ProviderPreset> {
    providers::all_presets()
}

/// 根据邮箱地址猜测服务商 id
#[tauri::command]
pub fn mail_guess_provider(email: String) -> String {
    providers::guess_provider(&email)
}

/// 测试连接：登录 IMAP 并读取文件夹列表，不写入任何数据
#[tauri::command]
pub async fn mail_test_connection(input: AddAccountInput) -> CmdResult<Vec<String>> {
    let cfg = input.into_config()?;
    tauri::async_runtime::spawn_blocking(move || imap::test_connection(&cfg))
        .await
        .map_err(|e| format!("任务执行失败：{e}"))?
}

/// 添加（或更新）一个真实邮箱账户
#[tauri::command]
pub async fn mail_add_account(
    app: tauri::AppHandle,
    token: String,
    input: AddAccountInput,
) -> CmdResult<super::types::MailboxAccount> {
    let label = if input.label.trim().is_empty() {
        input.email.trim().to_string()
    } else {
        input.label.trim().to_string()
    };
    let cfg = input.into_config()?;
    let crypto = crypto(&app)?;
    let user_id = server::user_id_from_token(&token).await?;

    // 先验证能登录成功，避免把错误的授权码存进数据库
    let probe = cfg.clone();
    tauri::async_runtime::spawn_blocking(move || imap::test_connection(&probe))
        .await
        .map_err(|e| format!("任务执行失败：{e}"))??;

    let client = server::open_client().await?;
    let account = store::upsert_account(&client, user_id, &cfg, &label, &crypto).await?;
    Ok(account)
}

/// 列出已接入的真实邮箱账户
#[tauri::command]
pub async fn mail_list_accounts(token: String) -> CmdResult<Vec<super::types::MailboxAccount>> {
    let user_id = server::user_id_from_token(&token).await?;
    let client = server::open_client().await?;
    store::list_accounts(&client, user_id).await
}

/// 移除一个邮箱账户（其邮件由前端一并清除）
#[tauri::command]
pub async fn mail_remove_account(token: String, account_id: String) -> CmdResult<()> {
    let user_id = server::user_id_from_token(&token).await?;
    let client = server::open_client().await?;
    store::delete_account(&client, user_id, &account_id).await
}

/// 同步结果（返回给前端）
#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SyncOutcome {
    pub account_id: String,
    pub emails: Vec<serde_json::Value>,
    pub skipped: usize,
    /// 是否还有更早的历史邮件待拉取。
    ///
    /// 邮箱可能有上万封邮件，单次同步只拉一部分（受 MAX_FETCH_PER_SYNC 限制），
    /// 剩余的靠后续同步逐批回填。前端据此提示用户「再点一次同步可加载更早的邮件」。
    pub has_more_history: bool,
    /// 该账户同步失败时的错误说明；成功时为 None。
    ///
    /// 保留为「部分成功」结构而非直接返回整体错误，是为了让一个账户的授权码失效
    /// 不会阻断其它账户的同步。前端据此能看到到底哪个账户出了问题。
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// 同步一个真实邮箱账户：拉取新邮件并写回游标
#[tauri::command]
pub async fn mail_sync_account(
    app: tauri::AppHandle,
    token: String,
    account_id: String,
) -> CmdResult<SyncOutcome> {
    let user_id = server::user_id_from_token(&token).await?;
    let crypto = crypto(&app)?;
    let client = server::open_client().await?;

    let cfg = store::load_config(&client, user_id, &account_id, &crypto).await?;
    let cursors = store::load_cursors(&client, user_id, &account_id).await?;

    // IMAP 是阻塞 IO，放到阻塞线程池执行，避免界面假死
    let fetch_cfg = cfg.clone();
    let result = tauri::async_runtime::spawn_blocking(move || imap::sync_account(&fetch_cfg, &cursors))
        .await
        .map_err(|e| format!("任务执行失败：{e}"))??;

    // 写回游标；失败不影响本次已拉到的邮件
    if let Err(e) = store::save_cursors(&client, user_id, &account_id, &result.cursors).await {
        eprintln!("[vela] 保存同步游标失败：{e}");
    }

    Ok(SyncOutcome {
        account_id,
        emails: result.mails.iter().map(|m| m.to_email_json()).collect(),
        skipped: result.skipped,
        // 任一文件夹还有历史待回填，就提示用户还能继续加载
        has_more_history: result.cursors.iter().any(|c| c.has_pending_history()),
        error: None,
    })
}

/// 同步所有真实邮箱账户
#[tauri::command]
pub async fn mail_sync_all(app: tauri::AppHandle, token: String) -> CmdResult<Vec<SyncOutcome>> {
    let user_id = server::user_id_from_token(&token).await?;
    let client = server::open_client().await?;
    let accounts = store::list_accounts(&client, user_id).await?;

    let mut out = Vec::new();
    for acc in accounts {
        match mail_sync_account(app.clone(), token.clone(), acc.id.clone()).await {
            Ok(r) => out.push(r),
            Err(e) => {
                // 单个账户失败不影响其它账户，但要把失败原因带回前端
                eprintln!("[vela] 账户 {} 同步失败：{e}", acc.id);
                out.push(SyncOutcome {
                    account_id: acc.id,
                    emails: Vec::new(),
                    skipped: 0,
                    has_more_history: false,
                    error: Some(e),
                });
            }
        }
    }
    Ok(out)
}

/// 回写已读 / 星标状态到邮件服务器
#[tauri::command]
pub async fn mail_set_flags(
    app: tauri::AppHandle,
    token: String,
    account_id: String,
    remote_name: String,
    uid: u32,
    read: Option<bool>,
    starred: Option<bool>,
) -> CmdResult<()> {
    let user_id = server::user_id_from_token(&token).await?;
    let crypto = crypto(&app)?;
    let client = server::open_client().await?;
    let cfg = store::load_config(&client, user_id, &account_id, &crypto).await?;

    tauri::async_runtime::spawn_blocking(move || {
        imap::set_flags(&cfg, &remote_name, uid, read, starred)
    })
    .await
    .map_err(|e| format!("任务执行失败：{e}"))?
}

/// 归档：移动到服务端的归档文件夹
#[tauri::command]
pub async fn mail_archive(
    app: tauri::AppHandle,
    token: String,
    account_id: String,
    remote_name: String,
    uid: u32,
) -> CmdResult<()> {
    mail_move(app, token, account_id, remote_name, uid, FolderKind::Archive).await
}

/// 删除：移动到服务端的垃圾箱
#[tauri::command]
pub async fn mail_trash(
    app: tauri::AppHandle,
    token: String,
    account_id: String,
    remote_name: String,
    uid: u32,
) -> CmdResult<()> {
    mail_move(app, token, account_id, remote_name, uid, FolderKind::Trash).await
}

/// 把邮件移动到指定类型的服务端文件夹
async fn mail_move(
    app: tauri::AppHandle,
    token: String,
    account_id: String,
    remote_name: String,
    uid: u32,
    kind: FolderKind,
) -> CmdResult<()> {
    let user_id = server::user_id_from_token(&token).await?;
    let crypto = crypto(&app)?;
    let client = server::open_client().await?;
    let cfg = store::load_config(&client, user_id, &account_id, &crypto).await?;

    tauri::async_runtime::spawn_blocking(move || {
        // 目标文件夹按服务商差异探测；找不到时给出明确提示而不是静默失败
        let target = imap::resolve_server_folder(&cfg, kind)?
            .ok_or_else(|| "未在邮箱中找到对应的文件夹（归档 / 垃圾箱），该邮箱可能不支持".to_string())?;
        imap::move_message(&cfg, &remote_name, uid, &target)
    })
    .await
    .map_err(|e| format!("任务执行失败：{e}"))?
}

/// 发信参数
#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SendInput {
    pub account_id: String,
    pub to: Vec<String>,
    #[serde(default)]
    pub cc: Vec<String>,
    #[serde(default)]
    pub in_reply_to: Option<String>,
    pub subject: String,
    pub body: String,
}

/// 通过 SMTP 真实发送一封邮件
#[tauri::command]
pub async fn mail_send(
    app: tauri::AppHandle,
    token: String,
    input: SendInput,
) -> CmdResult<String> {
    let user_id = server::user_id_from_token(&token).await?;
    let crypto = crypto(&app)?;
    let client = server::open_client().await?;
    let cfg = store::load_config(&client, user_id, &input.account_id, &crypto).await?;

    let mail = OutgoingMail {
        from_name: input.account_id.clone(),
        to: input.to,
        cc: input.cc,
        in_reply_to: input.in_reply_to,
        subject: input.subject,
        body: input.body,
    };

    tauri::async_runtime::spawn_blocking(move || smtp::send(&cfg, &mail))
        .await
        .map_err(|e| format!("任务执行失败：{e}"))?
}
