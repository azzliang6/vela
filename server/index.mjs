/**
 * Vela 同步服务 — 本地测试版
 * 职责：应用账户（注册/登录/会话）+ 每用户邮箱数据持久化到 PostgreSQL。
 * 生产化路线：作为 Tauri sidecar 打包，或迁移到 src-tauri 的 Rust (sqlx) 实现，接口不变。
 */
import express from "express";
import cors from "cors";
import bcrypt from "bcryptjs";
import { randomUUID } from "node:crypto";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import pg from "pg";

const __dirname = dirname(fileURLToPath(import.meta.url));
try {
  // 始终加载 server/.env（与 cwd 无关）
  process.loadEnvFile(join(__dirname, ".env"));
} catch {
  /* .env 不存在时使用默认值 */
}

const DB_URL = process.env.VELA_DB_URL ?? "postgres://postgres:postgres@localhost:5432/postgres";
const PORT = Number(process.env.VELA_PORT ?? 3210);

const pool = new pg.Pool({ connectionString: DB_URL, max: 5 });

async function initSchema() {
  await pool.query(`
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
  `);
}

/* ———— 行 <-> 前端 Email 结构互转 ———— */
function rowToEmail(r) {
  return {
    id: r.id,
    accountId: r.account_id,
    folder: r.folder,
    from: r.sender,
    to: r.recipients,
    cc: r.cc ?? undefined,
    subject: r.subject,
    body: r.body,
    date: new Date(r.date).toISOString(),
    read: r.read,
    starred: r.starred,
    category: r.category ?? null,
    aiSummary: r.ai_summary ?? undefined,
    aiWhy: r.ai_why ?? undefined,
    attachments: r.attachments ?? [],
    threadCount: r.thread_count,
  };
}

function emailToRow(e) {
  return [
    e.id,
    e.accountId,
    e.folder,
    JSON.stringify(e.from),
    JSON.stringify(e.to),
    e.cc ? JSON.stringify(e.cc) : null,
    e.subject,
    e.body,
    e.date,
    e.read,
    e.starred,
    e.category ?? null,
    e.aiSummary ?? null,
    e.aiWhy ?? null,
    JSON.stringify(e.attachments ?? []),
    e.threadCount ?? 1,
  ];
}

const COLS =
  "(user_id, id, account_id, folder, sender, recipients, cc, subject, body, date, read, starred, category, ai_summary, ai_why, attachments, thread_count)";

const app = express();
app.use(cors());
app.use(express.json({ limit: "8mb" }));

function fail(res, status, message) {
  res.status(status).json({ error: message });
}

/** Bearer 会话校验 */
async function auth(req, res, next) {
  const token = (req.headers.authorization ?? "").replace(/^Bearer\s+/i, "");
  if (!token) return fail(res, 401, "未登录");
  const { rows } = await pool.query(
    `SELECT u.id, u.username FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token = $1`,
    [token],
  );
  if (!rows.length) return fail(res, 401, "会话已失效");
  req.user = rows[0];
  req.token = token;
  next();
}

const USERNAME_RE = /^[\w\u4e00-\u9fa5.-]{2,30}$/;

app.post("/api/register", async (req, res) => {
  const { username, password } = req.body ?? {};
  if (!USERNAME_RE.test(String(username ?? ""))) return fail(res, 400, "用户名需 2-30 位字母/数字/中文/._-");
  if (String(password ?? "").length < 6) return fail(res, 400, "密码至少 6 位");
  const hash = await bcrypt.hash(String(password), 10);
  const client = await pool.connect();
  try {
    const { rows } = await client.query(
      "INSERT INTO users (username, pass_hash) VALUES ($1, $2) RETURNING id, username",
      [username, hash],
    );
    const token = randomUUID();
    await client.query("INSERT INTO sessions (token, user_id) VALUES ($1, $2)", [token, rows[0].id]);
    res.json({ token, username: rows[0].username });
  } catch (e) {
    if (e.code === "23505") return fail(res, 409, "用户名已被占用");
    throw e;
  } finally {
    client.release();
  }
});

app.post("/api/login", async (req, res) => {
  const { username, password } = req.body ?? {};
  const { rows } = await pool.query("SELECT id, username, pass_hash FROM users WHERE username = $1", [
    String(username ?? ""),
  ]);
  if (!rows.length || !(await bcrypt.compare(String(password ?? ""), rows[0].pass_hash))) {
    return fail(res, 401, "用户名或密码不正确");
  }
  const token = randomUUID();
  await pool.query("INSERT INTO sessions (token, user_id) VALUES ($1, $2)", [token, rows[0].id]);
  res.json({ token, username: rows[0].username });
});

app.post("/api/logout", auth, async (req, res) => {
  await pool.query("DELETE FROM sessions WHERE token = $1", [req.token]);
  res.json({ ok: true });
});

app.get("/api/me", auth, (req, res) => {
  res.json({ username: req.user.username });
});

app.get("/api/emails", auth, async (req, res) => {
  const { rows } = await pool.query(
    "SELECT * FROM emails WHERE user_id = $1 ORDER BY date DESC",
    [req.user.id],
  );
  res.json({ emails: rows.map(rowToEmail) });
});

/** 全量同步：以客户端列表为准 upsert，并删除缺失行 */
app.put("/api/emails/sync", auth, async (req, res) => {
  const emails = Array.isArray(req.body?.emails) ? req.body.emails : null;
  if (!emails) return fail(res, 400, "缺少 emails");
  if (emails.length > 2000) return fail(res, 400, "超出同步上限");
  const client = await pool.connect();
  try {
    await client.query("BEGIN");
    for (const e of emails) {
      const v = emailToRow(e);
      await client.query(
        `INSERT INTO emails ${COLS} VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
         ON CONFLICT (user_id, id) DO UPDATE SET
           account_id=EXCLUDED.account_id, folder=EXCLUDED.folder, sender=EXCLUDED.sender,
           recipients=EXCLUDED.recipients, cc=EXCLUDED.cc, subject=EXCLUDED.subject, body=EXCLUDED.body,
           date=EXCLUDED.date, read=EXCLUDED.read, starred=EXCLUDED.starred, category=EXCLUDED.category,
           ai_summary=EXCLUDED.ai_summary, ai_why=EXCLUDED.ai_why, attachments=EXCLUDED.attachments,
           thread_count=EXCLUDED.thread_count`,
        [req.user.id, ...v],
      );
    }
    if (emails.length) {
      await client.query(
        `DELETE FROM emails WHERE user_id = $1 AND id <> ALL($2::text[])`,
        [req.user.id, emails.map((e) => e.id)],
      );
    } else {
      await client.query("DELETE FROM emails WHERE user_id = $1", [req.user.id]);
    }
    await client.query("COMMIT");
    res.json({ synced: emails.length });
  } catch (e) {
    await client.query("ROLLBACK");
    throw e;
  } finally {
    client.release();
  }
});

// 统一错误出口
app.use((err, _req, res, _next) => {
  console.error("[vela-server]", err);
  fail(res, 500, "服务器内部错误");
});

await initSchema();
app.listen(PORT, "127.0.0.1", () => {
  console.log(`[vela-server] http://127.0.0.1:${PORT} · PG ready`);
});
