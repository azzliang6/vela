//! Vela 轻帆 — Tauri 桌面壳入口。
//!
//! 架构：纯本地单用户应用。邮件、账户凭证（AES-256-GCM 加密）、AI 分析
//! 结果全部存放在应用数据目录的单文件 SQLite 数据库里（`localdb`）。
//! IMAP/SMTP/AI 调用都在客户端本地完成（`mail`、前端 aiService），
//! 没有服务端组件。
//!
//! 前端通过 `invoke` 调用本文件注册的命令；浏览器开发模式下没有本地库，
//! 回退为演示数据（游客模式）。

/// 真实邮箱接入层（IMAP/SMTP）。
/// 声明为 `pub` 以便集成测试（tests/）能直接验证协议层行为。
pub mod mail;
mod ai;
mod localdb;

use tauri::Manager;

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
            mail::commands::db_get_emails,
            mail::commands::db_sync_emails,
            mail::commands::db_delete_emails,
            ai::ai_chat,
        ])
        .setup(|app| {
            // 打开（必要时创建）本地数据库：应用数据目录下的 vela.db
            let dir = app
                .path()
                .app_data_dir()
                .map_err(|e| Box::<dyn std::error::Error>::from(format!("无法确定应用数据目录：{e}")))?;
            let conn = localdb::open(&dir.join("vela.db"))
                .map_err(|e| Box::<dyn std::error::Error>::from(e))?;
            app.manage(localdb::Db(std::sync::Arc::new(std::sync::Mutex::new(conn))));
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
