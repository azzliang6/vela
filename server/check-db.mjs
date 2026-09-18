import { Client } from "pg";

const url = process.env.VELA_DB_URL ?? "postgres://postgres:1835696010%40Ljy@localhost:5432/postgres";
const client = new Client({ connectionString: url });
try {
  await client.connect();
  const v = await client.query("select version()");
  console.log("PG 连接成功:", v.rows[0].version.split(",")[0]);
  await client.end();
} catch (e) {
  console.error("PG 连接失败:", e.message);
  process.exit(1);
}
