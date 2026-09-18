import { Client } from "pg";

const c = new Client({
  connectionString: "postgres://postgres:1835696010%40Ljy@localhost:5432/postgres",
});
await c.connect();
const u = await c.query(
  "SELECT (SELECT count(*) FROM users) u, (SELECT count(*) FROM sessions) s, (SELECT count(*) FROM emails) e",
);
console.log("PG 落库验证 users/sessions/emails:", JSON.stringify(u.rows[0]));
const sample = await c.query(
  "SELECT subject, folder FROM emails WHERE subject LIKE '%持久化验证%' LIMIT 3",
);
console.log("发出的邮件行:", JSON.stringify(sample.rows));
const bcc = await c.query(
  "SELECT count(*)::int n FROM emails e JOIN users u ON u.id=e.user_id WHERE u.username LIKE '%_b'",
);
console.log("用户B邮箱行数(隔离):", bcc.rows[0].n);
await c.end();
