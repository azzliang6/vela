//! 后台新邮件监控（IMAP IDLE 实时推送）。
//!
//! 每个已接入的真实邮箱维持一条 IDLE 长连接：服务器一推送（RFC 2177）
//! 就做一次「仅增量」拉取，把新到的收件箱未读邮件以 `vela://new-mail`
//! 事件推给前端——前端负责合并状态与弹系统通知。邮件到达到弹通知的
//! 延迟从「轮询间隔（分钟级）」降到秒级。
//!
//! 生命周期：supervisor 周期性对账账户列表，新账户起监控任务；被删除
//! 的账户其任务在下一轮取配置失败时自行退出。监控任务断线后按指数
//! 退避重连（15s 起、上限 5 分钟），成功的拉取会把退避重置回起点。
//!
//! 数据库锁只在读写配置与游标时短暂持有，绝不跨 IDLE 网络等待。

use std::time::Duration;

use tauri::{AppHandle, Emitter, Manager};

use super::commands::crypto;
use super::types::MailboxCursor;
use super::{imap, store};
use crate::localdb::Db;

/// 单次 IDLE 最长等待，到期重查增量再重新 IDLE。
/// RFC 2177 建议客户端至少每 29 分钟重发一次 IDLE，10 分钟留足余量。
const IDLE_WAIT: Duration = Duration::from_secs(10 * 60);

/// supervisor 对账账户列表的周期
const RESCAN: Duration = Duration::from_secs(45);

/// 断线重连的起始退避与上限
const BACKOFF_START: Duration = Duration::from_secs(15);
const BACKOFF_MAX: Duration = Duration::from_secs(5 * 60);

/// 推给前端的「新邮件到达」事件载荷
#[derive(Clone, serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct NewMailEvent {
    pub account_id: String,
    /// 新到邮件的前端 JSON（`to_email_json` 产物，均带 isNewArrival = true）
    pub emails: Vec<serde_json::Value>,
}

/// 启动监控 supervisor（应用 setup 时调用一次，仅桌面端）。
pub fn spawn(app: AppHandle) {
    tauri::async_runtime::spawn(async move {
        // 账户 id → 是否已有监控任务在跑（存在性标记）。
        // 监控任务内部不 unwrap、错误全部走退避分支，正常不会 panic；
        // 万一异常退出，重启应用即可恢复（轮询同步不受影响）。
        let mut running: std::collections::HashSet<String> = std::collections::HashSet::new();
        loop {
            let accounts = current_accounts(&app);
            let live: std::collections::HashSet<&String> = accounts.iter().collect();
            // 账户被删除的，标记清掉（其任务会在下一轮取配置时自行退出）
            running.retain(|id| live.contains(id));
            for id in accounts {
                if running.insert(id.clone()) {
                    tauri::async_runtime::spawn(watch_account(app.clone(), id));
                }
            }
            tokio::time::sleep(RESCAN).await;
        }
    });
}

/// 单个账户的监控循环：等 IDLE → 拉增量 → 存游标 → 通知前端，循环往复。
async fn watch_account(app: AppHandle, account_id: String) {
    let mut backoff = BACKOFF_START;
    loop {
        // 账户被删除后 load_config 失败，任务自然退出
        let (cfg, cursors) = {
            let Some(db) = app.try_state::<Db>() else { return };
            let Ok(conn) = db.0.lock() else { return };
            let Ok(crypto) = crypto(&app) else { return };
            match store::load_config(&conn, &account_id, &crypto) {
                Ok(cfg) => (cfg, store::load_cursors(&conn, &account_id).unwrap_or_default()),
                Err(_) => {
                    eprintln!("[vela] 实时监控退出：账户 {account_id} 已不可用");
                    return;
                }
            }
        };

        // 无新邮件时一直阻塞在 IDLE 里（阻塞线程池），直到有新邮件或出错
        let wait_cursors = cursors.clone();
        let outcome = tauri::async_runtime::spawn_blocking(move || {
            imap::idle_fetch_new(&cfg, &wait_cursors, IDLE_WAIT)
        })
        .await;

        match outcome {
            Ok(Ok(result)) => {
                backoff = BACKOFF_START;
                save_merged_cursors(&app, &account_id, &cursors, &result.cursors);
                let emails: Vec<serde_json::Value> = result
                    .mails
                    .iter()
                    .filter(|m| m.is_new_arrival)
                    .map(|m| m.to_email_json())
                    .collect();
                if !emails.is_empty() {
                    if let Err(e) = app.emit(
                        "vela://new-mail",
                        NewMailEvent {
                            account_id: account_id.clone(),
                            emails,
                        },
                    ) {
                        eprintln!("[vela] 推送新邮件事件失败：{e}");
                    }
                }
                // 拉到了新邮件：立即进入下一轮（先查增量再 IDLE）
            }
            Ok(Err(e)) => {
                eprintln!("[vela] {account_id} 实时监控断开：{e}，{backoff:?} 后重连");
                tokio::time::sleep(backoff).await;
                backoff = std::cmp::min(backoff * 2, BACKOFF_MAX);
            }
            Err(e) => {
                eprintln!("[vela] {account_id} 监控任务异常：{e}");
                tokio::time::sleep(backoff).await;
                backoff = std::cmp::min(backoff * 2, BACKOFF_MAX);
            }
        }
    }
}

/// 把实时通道拿回的游标合并进既有游标再写回。
///
/// - 实时通道只带回收件箱游标，已发送等其它文件夹的必须原样保留，
///   否则下一次常规同步会把它们从头拉一遍
/// - 同一文件夹按更大的 last_uid 取胜：与前端的常规同步并发推进游标时，
///   谁都不把对方往回退（回退导致的重复拉取虽然幂等，但没有必要）
fn save_merged_cursors(
    app: &AppHandle,
    account_id: &str,
    before: &[MailboxCursor],
    fetched: &[MailboxCursor],
) {
    let Some(db) = app.try_state::<Db>() else { return };
    let Ok(conn) = db.0.lock() else { return };

    let mut merged: Vec<MailboxCursor> = before.to_vec();
    for c in fetched {
        match merged.iter_mut().find(|m| m.folder == c.folder) {
            Some(m) if c.last_uid < m.last_uid => {}
            Some(m) => *m = c.clone(),
            None => merged.push(c.clone()),
        }
    }
    if let Err(e) = store::save_cursors(&conn, account_id, &merged) {
        eprintln!("[vela] 保存实时同步游标失败：{e}");
    }
}

/// 当前已接入的真实邮箱账户 id 列表（数据库不可用时返回空，本轮跳过）
fn current_accounts(app: &AppHandle) -> Vec<String> {
    let Some(db) = app.try_state::<Db>() else { return Vec::new() };
    let Ok(conn) = db.0.lock() else { return Vec::new() };
    store::list_accounts(&conn)
        .unwrap_or_default()
        .into_iter()
        .map(|a| a.id)
        .collect()
}
