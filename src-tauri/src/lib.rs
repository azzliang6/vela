//! Vela 轻帆 — Tauri 桌面壳入口。
//!
//! 架构：纯本地单用户应用。邮件、账户凭证（AES-256-GCM 加密）、AI 分析
//! 结果全部存放在应用数据目录的单文件 SQLite 数据库里（`localdb`）。
//! IMAP/SMTP/AI 调用都在客户端本地完成（`mail`、前端 aiService），
//! 没有服务端组件。
//!
//! 前端通过 `invoke` 调用本文件注册的命令；浏览器开发模式下没有本地库，
//! 回退为演示数据（游客模式）。
//!
//! 桌面端常驻策略：关窗只是隐藏到系统托盘（新邮件实时监控在后台继续），
//! 真正退出走托盘菜单「退出」。

/// 真实邮箱接入层（IMAP/SMTP）。
/// 声明为 `pub` 以便集成测试（tests/）能直接验证协议层行为。
pub mod mail;
mod ai;
mod localdb;

use tauri::Manager;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_notification::init())
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

            // 系统托盘 + 新邮件实时监控（仅桌面端）
            #[cfg(desktop)]
            {
                setup_tray(app)?;
                mail::watcher::spawn(app.handle().clone());
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            // 关窗 = 隐藏到托盘（实时监控继续）；真正退出走托盘菜单「退出」
            #[cfg(desktop)]
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                if window.label() == "main" {
                    let _ = window.hide();
                    api.prevent_close();
                }
            }
            #[cfg(not(desktop))]
            let _ = (window, event);
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

/// 创建系统托盘：菜单（显示 / 立即同步 / 退出）+ 左键单击唤起主窗口。
#[cfg(desktop)]
fn setup_tray(app: &tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    use tauri::menu::{Menu, MenuItem};
    use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};

    let show = MenuItem::with_id(app, "show", "显示主窗口", true, None::<&str>)?;
    let sync = MenuItem::with_id(app, "sync", "立即同步", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&show, &sync, &quit])?;

    let icon = app
        .default_window_icon()
        .ok_or("未找到应用图标，无法创建系统托盘")?
        .clone();

    TrayIconBuilder::with_id("vela-tray")
        .icon(icon)
        .tooltip("Vela 轻帆 · 新邮件实时监控中")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id().as_ref() {
            "show" => show_main_window(app),
            "sync" => {
                use tauri::Emitter;
                // 同步的编排（合并状态、提示、游标对账）都在前端，这里只转发意图
                if let Err(e) = app.emit("vela://tray-sync", ()) {
                    eprintln!("[vela] 转发托盘同步事件失败：{e}");
                }
            }
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_main_window(tray.app_handle());
            }
        })
        .build(app)?;
    Ok(())
}

/// 显示并聚焦主窗口
#[cfg(desktop)]
fn show_main_window(app: &tauri::AppHandle) {
    if let Some(win) = app.get_webview_window("main") {
        let _ = win.show();
        let _ = win.unminimize();
        let _ = win.set_focus();
    }
}
