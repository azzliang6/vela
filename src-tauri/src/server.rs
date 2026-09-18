//! Vela 内嵌同步服务。
//!
//! 随桌面应用进程启动，在 127.0.0.1:3210 提供与 `server/`（Node 版）完全一致的 REST API：
//! 注册 / 登录 / 会话 / 邮件全量同步。前端在浏览器与桌面两种形态下使用同一套代码。
//!
//! 数据库连接按以下优先级解析：
//! 1. 环境变量 `VELA_DB_URL`
//! 2. 可执行文件同目录的 `vela.config.json`
//! 3. 应用配置目录的 `config.json`
//! 4. 内置默认（本机开发库）
//!
//! 数据库不可用时仅记录日志，应用与「游客模式」不受影响。

use std::path::{Path, PathBuf};
use std::str::FromStr;
use std::sync::Arc;

use axum::{
    extract::{DefaultBodyLimit, State},
    http::{HeaderMap, StatusCode},
    response::{IntoResponse, Response},
    routing::{get, post, put},
    Json, Router,
};
use serde::Deserialize;
use serde_json::{json, Value};
use tokio_postgres::types::ToSql;
use tokio_postgres::{Client, Config as PgConfig, NoTls};
use tower_http::cors::CorsLayer;

const PORT: u16 = 3210;
const BODY_LIMIT: usize = 16 * 1024 * 1024;

/// 参数数组元素类型别名（tokio-postgres 需要显式 dyn 转换）
type P<'a> = &'a (dyn ToSql + Sync);

#[derive(Clone)]
pub(crate) enum Db {
    Dsn(String),
    Parts {
        host: String,
        port: u16,
        user: String,
        password: String,
        database: String,
    },
}

impl Db {
    fn pg_config(&self) -> Result<PgConfig, String> {
        match self {
            Db::Dsn(s) => PgConfig::from_str(s).map_err(|e| format!("连接串无效：{e}")),
            Db::Parts {
                host,
                port,
                user,
                password,
                database,
            } => {
                let mut c = PgConfig::new();
                // 程序化构造避免密码中特殊字符（如 @）的转义问题
                c.host(host)
                    .port(*port)
                    .user(user)
                    .password(password.clone())
                    .dbname(database);
                Ok(c)
            }
        }
    }

    fn label(&self) -> String {
        match self {
            Db::Dsn(_) => "连接串配置".to_string(),
            Db::Parts {
                host,
                port,
                database,
                ..
            } => format!("{host}:{port}/{database}"),
        }
    }
}

pub(crate) struct AppState {
    pub(crate) db: Db,
}

/* ———————————————— 配置解析 ———————————————— */

fn load_db(app: &tauri::AppHandle) -> Db {
    if let Ok(v) = std::env::var("VELA_DB_URL") {
        let v = v.trim().to_string();
        if !v.is_empty() {
            return Db::Dsn(v);
        }
    }
    let mut candidates: Vec<PathBuf> = Vec::new();
    if let Ok(exe) = std::env::current_exe() {
        if let Some(dir) = exe.parent() {
            candidates.push(dir.join("vela.config.json"));
        }
    }
    if let Ok(dir) = tauri::Manager::path(app).app_config_dir() {
        candidates.push(dir.join("config.json"));
    }
    for p in candidates {
        if let Some(db) = read_config(&p) {
            return db;
        }
    }
    Db::Parts {
        host: "localhost".into(),
        port: 5432,
        user: "postgres".into(),
        password: "1835696010@Ljy".into(),
        database: "postgres".into(),
    }
}

fn read_config(path: &Path) -> Option<Db> {
    let txt = std::fs::read_to_string(path).ok()?;
    let v: Value = serde_json::from_str(&txt).ok()?;
    for key in ["dsn", "VELA_DB_URL"] {
        if let Some(dsn) = v.get(key).and_then(|x| x.as_str()) {
            if !dsn.trim().is_empty() {
                return Some(Db::Dsn(dsn.to_string()));
            }
        }
    }
    let host = v.get("host").and_then(|x| x.as_str())?;
    Some(Db::Parts {
        host: host.to_string(),
        port: v.get("port").and_then(|x| x.as_u64()).unwrap_or(5432) as u16,
        user: v
            .get("user")
            .and_then(|x| x.as_str())
            .unwrap_or("postgres")
            .to_string(),
        password: v
            .get("password")
            .and_then(|x| x.as_str())
            .unwrap_or("")
            .to_string(),
        database: v
            .get("database")
            .and_then(|x| x.as_str())
            .unwrap_or("postgres")
            .to_string(),
    })
}

/* ———————————————— 数据库 ———————————————— */

const SCHEMA: &str = r#"
CREATE TABLE IF NOT EXISTS users (
  id         SERIAL PRIMARY KEY,
  username   TEXT NOT NULL UNIQUE,
  pass_hash  TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS sessions (
  token      TEXT PRIMARY KEY,
  user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS emails (
  user_id      INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  id           TEXT NOT NULL,
  account_id   TEXT NOT NULL,
  folder       TEXT NOT NULL,
  sender       JSONB NOT NULL,
  recipients   JSONB NOT NULL,
  cc           JSONB,
  subject      TEXT NOT NULL,
  body         TEXT NOT NULL,
  date         TIMESTAMPTZ NOT NULL,
  read         BOOLEAN NOT NULL DEFAULT false,
  starred      BOOLEAN NOT NULL DEFAULT false,
  category     TEXT,
  ai_summary   TEXT,
  ai_why       TEXT,
  attachments  JSONB NOT NULL DEFAULT '[]',
  thread_count INT NOT NULL DEFAULT 1,
  PRIMARY KEY (user_id, id)
);
"#;

async fn connect(db: &Db) -> Result<Client, String> {
    let cfg = db.pg_config()?;
    let (client, connection) = cfg
        .connect(NoTls)
        .await
        .map_err(|e| format!("数据库连接失败：{e}"))?;
    tokio::spawn(async move {
        let _ = connection.await;
    });
    Ok(client)
}

/* ———— 供邮件模块（mail）复用的入口 ———— */

/// 全局数据库配置。由 `start()` 初始化，供 mail 模块的命令处理器取用。
static SHARED_DB: std::sync::OnceLock<Db> = std::sync::OnceLock::new();

/// 取得数据库配置；`start()` 尚未执行时返回 None。
pub(crate) fn shared_db() -> Option<Db> {
    SHARED_DB.get().cloned()
}

/// 打开一条数据库连接
pub(crate) async fn open_client() -> Result<Client, String> {
    match shared_db() {
        Some(db) => connect(&db).await,
        None => Err("数据库尚未就绪，请稍后重试".into()),
    }
}

/// 由 Bearer 令牌解析用户 id，供 mail 模块复用
pub(crate) async fn user_id_from_token(token: &str) -> Result<i32, String> {
    let client = open_client().await?;
    let row = client
        .query_opt(
            "SELECT u.id FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token = $1",
            &[&token as P],
        )
        .await
        .map_err(|e| format!("校验会话失败：{e}"))?;
    match row {
        Some(r) => Ok(r.get::<_, i32>(0)),
        None => Err("会话已失效，请重新登录".into()),
    }
}

async fn init_schema(db: &Db) -> Result<(), String> {
    let client = connect(db).await?;
    client
        .batch_execute(SCHEMA)
        .await
        .map_err(|e| format!("建表失败：{e}"))?;
    // 真实邮箱账户表（凭证加密存储）
    client
        .batch_execute(crate::mail::store::SCHEMA)
        .await
        .map_err(|e| format!("创建邮箱账户表失败：{e}"))?;
    Ok(())
}

/* ———————————————— 错误 ———————————————— */

struct ApiErr(StatusCode, String);

impl IntoResponse for ApiErr {
    fn into_response(self) -> Response {
        (self.0, Json(json!({ "error": self.1 }))).into_response()
    }
}

fn internal<E: std::fmt::Display>(e: E) -> ApiErr {
    eprintln!("[vela] server error: {e}");
    ApiErr(StatusCode::INTERNAL_SERVER_ERROR, "服务器内部错误".into())
}

fn bad(msg: &str) -> ApiErr {
    ApiErr(StatusCode::BAD_REQUEST, msg.into())
}

/* ———————————————— 鉴权 ———————————————— */

async fn require_user(db: &Db, headers: &HeaderMap) -> Result<(i32, String), ApiErr> {
    let raw = headers
        .get("authorization")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    let token = raw
        .split_once(' ')
        .map(|(_, t)| t.trim())
        .filter(|t| !t.is_empty())
        .ok_or(ApiErr(StatusCode::UNAUTHORIZED, "未登录".into()))?;

    let client = connect(db).await.map_err(internal)?;
    let row = client
        .query_opt(
            "SELECT u.id, u.username FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token = $1",
            &[&token as P],
        )
        .await
        .map_err(internal)?;
    match row {
        Some(r) => Ok((r.get::<_, i32>(0), r.get::<_, String>(1))),
        None => Err(ApiErr(StatusCode::UNAUTHORIZED, "会话已失效".into())),
    }
}

/* ———————————————— 请求体 ———————————————— */

#[derive(Deserialize)]
struct Creds {
    username: String,
    password: String,
}

#[derive(Deserialize)]
struct SyncBody {
    emails: Vec<Value>,
}

fn valid_username(name: &str) -> bool {
    let n = name.chars().count();
    (2..=30).contains(&n)
        && name
            .chars()
            .all(|c| c.is_alphanumeric() || matches!(c, '.' | '_' | '-'))
}

/* ———————————————— 处理器 ———————————————— */

async fn register(
    State(st): State<Arc<AppState>>,
    Json(c): Json<Creds>,
) -> Result<Json<Value>, ApiErr> {
    let username = c.username.trim().to_string();
    if !valid_username(&username) {
        return Err(bad("用户名需 2-30 位字母/数字/中文/._-"));
    }
    if c.password.chars().count() < 6 {
        return Err(bad("密码至少 6 位"));
    }
    let hash = bcrypt::hash(&c.password, 10).map_err(internal)?;
    let client = connect(&st.db).await.map_err(internal)?;

    let row = client
        .query_opt(
            "INSERT INTO users (username, pass_hash) VALUES ($1, $2)
             ON CONFLICT (username) DO NOTHING RETURNING id",
            &[&username as P, &hash as P],
        )
        .await
        .map_err(internal)?;
    let Some(r) = row else {
        return Err(ApiErr(StatusCode::CONFLICT, "用户名已被占用".into()));
    };
    let user_id: i32 = r.get(0);

    let token = uuid::Uuid::new_v4().to_string();
    client
        .execute(
            "INSERT INTO sessions (token, user_id) VALUES ($1, $2)",
            &[&token as P, &user_id as P],
        )
        .await
        .map_err(internal)?;

    Ok(Json(json!({ "token": token, "username": username })))
}

async fn login(
    State(st): State<Arc<AppState>>,
    Json(c): Json<Creds>,
) -> Result<Json<Value>, ApiErr> {
    let client = connect(&st.db).await.map_err(internal)?;
    let row = client
        .query_opt(
            "SELECT id, username, pass_hash FROM users WHERE username = $1",
            &[&c.username.trim() as P],
        )
        .await
        .map_err(internal)?;

    let unauthorized = || ApiErr(StatusCode::UNAUTHORIZED, "用户名或密码不正确".into());
    let Some(r) = row else {
        return Err(unauthorized());
    };
    let user_id: i32 = r.get(0);
    let username: String = r.get(1);
    let hash: String = r.get(2);
    if !bcrypt::verify(&c.password, &hash).unwrap_or(false) {
        return Err(unauthorized());
    }

    let token = uuid::Uuid::new_v4().to_string();
    client
        .execute(
            "INSERT INTO sessions (token, user_id) VALUES ($1, $2)",
            &[&token as P, &user_id as P],
        )
        .await
        .map_err(internal)?;

    Ok(Json(json!({ "token": token, "username": username })))
}

async fn logout(
    State(st): State<Arc<AppState>>,
    headers: HeaderMap,
) -> Result<Json<Value>, ApiErr> {
    let raw = headers
        .get("authorization")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    let token = raw
        .split_once(' ')
        .map(|(_, t)| t.trim())
        .filter(|t| !t.is_empty())
        .ok_or(ApiErr(StatusCode::UNAUTHORIZED, "未登录".into()))?;
    let _ = require_user(&st.db, &headers).await?;

    let client = connect(&st.db).await.map_err(internal)?;
    client
        .execute("DELETE FROM sessions WHERE token = $1", &[&token as P])
        .await
        .map_err(internal)?;
    Ok(Json(json!({ "ok": true })))
}

async fn me(
    State(st): State<Arc<AppState>>,
    headers: HeaderMap,
) -> Result<Json<Value>, ApiErr> {
    let (_, username) = require_user(&st.db, &headers).await?;
    Ok(Json(json!({ "username": username })))
}

/// 行 -> 前端 Email 结构的 JSONB 投影（字段名与前端约定一致）
const EMAIL_SELECT: &str = r#"
SELECT jsonb_build_object(
  'id', id, 'accountId', account_id, 'folder', folder,
  'from', sender, 'to', recipients, 'cc', cc,
  'subject', subject, 'body', body, 'date', to_jsonb(date),
  'read', read, 'starred', starred, 'category', category,
  'aiSummary', ai_summary, 'aiWhy', ai_why,
  'attachments', attachments, 'threadCount', thread_count
) AS email
FROM emails WHERE user_id = $1 ORDER BY date DESC
"#;

async fn list_emails(
    State(st): State<Arc<AppState>>,
    headers: HeaderMap,
) -> Result<Json<Value>, ApiErr> {
    let (user_id, _) = require_user(&st.db, &headers).await?;
    let client = connect(&st.db).await.map_err(internal)?;
    let rows = client
        .query(EMAIL_SELECT, &[&user_id as P])
        .await
        .map_err(internal)?;
    let emails: Vec<Value> = rows.iter().map(|r| r.get::<_, Value>(0)).collect();
    Ok(Json(json!({ "emails": emails })))
}

/// 单封邮件 upsert：整封作为 JSONB 参数，由 SQL 投影到各列
const EMAIL_UPSERT: &str = r#"
INSERT INTO emails (
  user_id, id, account_id, folder, sender, recipients, cc, subject, body,
  date, read, starred, category, ai_summary, ai_why, attachments, thread_count
)
SELECT $1,
  j->>'id', j->>'accountId', j->>'folder', j->'from', j->'to',
  nullif(j->'cc', 'null'::jsonb),
  coalesce(j->>'subject', ''), coalesce(j->>'body', ''),
  (j->>'date')::timestamptz,
  coalesce((j->>'read')::boolean, false), coalesce((j->>'starred')::boolean, false),
  j->>'category', j->>'aiSummary', j->>'aiWhy',
  coalesce(j->'attachments', '[]'::jsonb), coalesce((j->>'threadCount')::int, 1)
FROM (SELECT $2::jsonb AS j) t
ON CONFLICT (user_id, id) DO UPDATE SET
  account_id = EXCLUDED.account_id, folder = EXCLUDED.folder, sender = EXCLUDED.sender,
  recipients = EXCLUDED.recipients, cc = EXCLUDED.cc, subject = EXCLUDED.subject,
  body = EXCLUDED.body, date = EXCLUDED.date, read = EXCLUDED.read,
  starred = EXCLUDED.starred, category = EXCLUDED.category,
  ai_summary = EXCLUDED.ai_summary, ai_why = EXCLUDED.ai_why,
  attachments = EXCLUDED.attachments, thread_count = EXCLUDED.thread_count
"#;

async fn sync_emails(
    State(st): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(body): Json<SyncBody>,
) -> Result<Json<Value>, ApiErr> {
    let (user_id, _) = require_user(&st.db, &headers).await?;
    if body.emails.len() > 2000 {
        return Err(bad("超出同步上限"));
    }

    let mut client = connect(&st.db).await.map_err(internal)?;
    let tx = client.transaction().await.map_err(internal)?;

    for e in &body.emails {
        tx.execute(EMAIL_UPSERT, &[&user_id as P, e as P])
            .await
            .map_err(internal)?;
    }

    if body.emails.is_empty() {
        tx.execute("DELETE FROM emails WHERE user_id = $1", &[&user_id as P])
            .await
            .map_err(internal)?;
    } else {
        let ids: Vec<String> = body
            .emails
            .iter()
            .filter_map(|e| e.get("id").and_then(|v| v.as_str()).map(str::to_string))
            .collect();
        tx.execute(
            "DELETE FROM emails WHERE user_id = $1 AND id <> ALL($2)",
            &[&user_id as P, &ids as P],
        )
        .await
        .map_err(internal)?;
    }

    tx.commit().await.map_err(internal)?;
    Ok(Json(json!({ "synced": body.emails.len() })))
}

/* ———————————————— 启动 ———————————————— */

pub async fn start(app: tauri::AppHandle) {
    let db = load_db(&app);
    let _ = SHARED_DB.set(db.clone());
    match init_schema(&db).await {
        Ok(()) => eprintln!("[vela] 数据库就绪：{}", db.label()),
        Err(e) => eprintln!(
            "[vela] 数据库不可用（{e}）—— 登录功能不可用，游客模式不受影响；\
             可配置 VELA_DB_URL 或 vela.config.json 后重启"
        ),
    }

    let router = Router::new()
        .route("/api/register", post(register))
        .route("/api/login", post(login))
        .route("/api/logout", post(logout))
        .route("/api/me", get(me))
        .route("/api/emails", get(list_emails))
        .route("/api/emails/sync", put(sync_emails))
        .layer(DefaultBodyLimit::max(BODY_LIMIT))
        .layer(CorsLayer::permissive())
        .with_state(Arc::new(AppState { db }));

    match tokio::net::TcpListener::bind(("127.0.0.1", PORT)).await {
        Ok(listener) => {
            eprintln!("[vela] 内嵌同步服务已启动 http://127.0.0.1:{PORT}");
            if let Err(e) = axum::serve(listener, router).await {
                eprintln!("[vela] 内嵌服务退出：{e}");
            }
        }
        Err(e) => eprintln!(
            "[vela] 端口 {PORT} 被占用（{e}）；若已有同步服务在运行（如 pnpm server），不影响使用"
        ),
    }
}
