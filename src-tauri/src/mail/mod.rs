//! 真实邮箱接入层。
//!
//! 子模块：
//! - [`providers`]：QQ / 163 / Gmail 等服务商预设，自动填服务器地址
//! - [`crypto`]：授权码的 AES-256-GCM 加密，数据库里只存密文
//! - [`store`]：邮箱账户与同步游标的 PostgreSQL 存取
//! - [`imap`]：IMAP 收信（增量拉取）+ 已读/星标/归档/删除回写
//! - [`smtp`]：SMTP 发信
//! - [`commands`]：暴露给前端的 Tauri command
//!
//! 数据流：
//! ```text
//! 前端 invoke("mail_sync_account")
//!   → commands（异步，不阻塞 UI）
//!     → store 取账户 + 解密授权码
//!     → imap 拉新邮件
//!     → store 写回游标
//!   → 返回邮件 JSON 数组给前端
//! ```

pub mod commands;
pub mod crypto;
pub mod imap;
pub mod providers;
pub mod smtp;
pub mod store;
pub mod types;

pub use crypto::Crypto;
