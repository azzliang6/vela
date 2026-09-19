/**
 * 本地邮件库访问层。
 *
 * 应用是纯本地单用户形态：邮件与状态存放在应用数据目录的 SQLite 文件里
 * （`~/.local/share/app.vela.mail/vela.db`），通过 Tauri 命令
 * （`mail::commands::db_*`）读写。没有服务端、没有账号体系。
 *
 * 浏览器开发模式（`pnpm dev`）没有本地库，MailContext 会回退为游客演示数据。
 */

/** 本地库返回的邮件形状（与前端 `Email` 字段名对齐） */
export interface ServerEmail {
  id: string;
  accountId: string;
  folder: string;
  from: { name: string; email: string };
  to: { name: string; email: string }[];
  cc?: { name: string; email: string }[];
  subject: string;
  body: string;
  htmlBody?: string | null;
  date: string;
  read: boolean;
  starred: boolean;
  category: string | null;
  aiSummary?: string;
  aiWhy?: string;
  attachments: unknown[];
  threadCount: number;
}

/** 是否运行在 Tauri 桌面 / 移动壳里 */
export function isDesktop(): boolean {
  const w = window as unknown as Record<string, unknown>;
  return Boolean(w.__TAURI_INTERNALS__ || w.__TAURI__);
}

async function invoke<T>(cmd: string, args: Record<string, unknown>): Promise<T> {
  const mod = await import("@tauri-apps/api/core");
  return mod.invoke<T>(cmd, args);
}

/** 读取本地全部邮件（含 AI 分析结果），按时间倒序 */
export async function getEmails(): Promise<ServerEmail[]> {
  return invoke<ServerEmail[]>("db_get_emails", {});
}

/** 全量 upsert 本地邮件（防抖保存：以界面内存状态为准） */
export async function syncEmails(emails: unknown[]): Promise<void> {
  await invoke("db_sync_emails", { emails });
}

/** 删除指定邮件（对账剔除 / 清空垃圾箱 / 移除账户时） */
export async function deleteEmails(ids: string[]): Promise<void> {
  if (!ids.length) return;
  await invoke("db_delete_emails", { ids });
}
