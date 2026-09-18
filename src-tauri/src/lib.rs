//! Vela 轻帆 — Tauri 桌面壳入口。
//!
//! 组成：
//! - `server`：内嵌同步服务（账户注册登录 + 邮件持久化到 PostgreSQL），监听 127.0.0.1:3210
//! - `mail`：真实邮箱接入（IMAP 收信 / SMTP 发信 / 已读星标回写），以 Tauri command 形式供前端调用
//!
//! 前端在浏览器与桌面两种形态下共用同一套代码：桌面环境通过 `invoke` 调用 `mail_*` 命令，
//! 浏览器环境则回退到 Node 版同步服务。

/// 真实邮箱接入层（IMAP/SMTP）。
/// 声明为 `pub` 以便集成测试（tests/）能直接验证协议层行为。
pub mod mail;
mod server;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            mail::commands::mail_providers,
            mail::commands::mail_guess_provider,
            mail::commands::mail_test_connection,
            mail::commands::mail_add_account,
            mail::commands::mail_list_accounts,
            mail::commands::mail_remove_account,
            mail::commands::mail_sync_account,
            mail::commands::mail_sync_all,
            mail::commands::mail_set_flags,
            mail::commands::mail_archive,
            mail::commands::mail_trash,
            mail::commands::mail_send,
        ])
        .setup(|app| {
            let handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                server::start(handle).await;
            });
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
