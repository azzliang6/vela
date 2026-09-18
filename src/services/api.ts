/** Vela 同步服务 API 封装（本地测试服务 http://127.0.0.1:3210） */

const BASE = "http://127.0.0.1:3210";

interface ReqOpts {
  method?: string;
  body?: unknown;
  token?: string;
}

async function request<T>(path: string, opts: ReqOpts = {}): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${BASE}${path}`, {
      method: opts.method ?? "GET",
      headers: {
        "Content-Type": "application/json",
        ...(opts.token ? { Authorization: `Bearer ${opts.token}` } : {}),
      },
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    });
  } catch {
    throw new Error("无法连接同步服务，请先运行 pnpm server");
  }
  const data = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) throw new Error((data.error as string) ?? `请求失败 (${res.status})`);
  return data as T;
}

export interface ServerEmail {
  id: string;
  accountId: string;
  folder: string;
  from: { name: string; email: string };
  to: { name: string; email: string }[];
  cc?: { name: string; email: string }[];
  subject: string;
  body: string;
  date: string;
  read: boolean;
  starred: boolean;
  category: string | null;
  aiSummary?: string;
  aiWhy?: string;
  attachments: unknown[];
  threadCount: number;
}

export const api = {
  register: (username: string, password: string) =>
    request<{ token: string; username: string }>("/api/register", {
      method: "POST",
      body: { username, password },
    }),
  login: (username: string, password: string) =>
    request<{ token: string; username: string }>("/api/login", {
      method: "POST",
      body: { username, password },
    }),
  logout: (token: string) =>
    request<{ ok: boolean }>("/api/logout", { method: "POST", token }),
  getMe: (token: string) =>
    request<{ username: string }>("/api/me", { token }),
  getEmails: (token: string) =>
    request<{ emails: ServerEmail[] }>("/api/emails", { token }),
  syncEmails: (token: string, emails: unknown[]) =>
    request<{ synced: number }>("/api/emails/sync", {
      method: "PUT",
      token,
      body: { emails },
    }),
};
