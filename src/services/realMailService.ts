/**
 * 真实邮箱服务客户端。
 *
 * 运行环境有两套后端，这里自动选择：
 *
 * | 环境 | 后端 | 说明 |
 * | --- | --- | --- |
 * | Tauri 桌面应用 | Rust command（`invoke`） | IMAP/SMTP 在 Rust 里实现 |
 * | 浏览器（`pnpm dev`） | Node 服务 `server/index.mjs` | 走 HTTP 到 127.0.0.1:3210 |
 *
 * 桌面环境才是完整功能；浏览器环境因为无法做原生 TCP，会明确提示用户改用桌面版。
 */

import type { Email, ProviderPreset, RealMailbox } from "../types";

/** 是否运行在 Tauri 桌面壳里 */
export function isDesktop(): boolean {
  // Tauri 2 会注入 __TAURI_INTERNALS__；老版本用 __TAURI__
  const w = window as unknown as Record<string, unknown>;
  return Boolean(w.__TAURI_INTERNALS__ || w.__TAURI__);
}

/** 动态引入 Tauri 的 invoke，避免浏览器环境打包时报错 */
async function invoke<T>(cmd: string, args: Record<string, unknown>): Promise<T> {
  const mod = await import("@tauri-apps/api/core");
  return mod.invoke<T>(cmd, args);
}

/** 真实邮箱能力是否可用（仅桌面版） */
export function realMailSupported(): boolean {
  return isDesktop();
}

const NOT_DESKTOP_MSG =
  "真实邮箱接入需要在桌面应用中使用（IMAP/SMTP 需要原生网络能力）。请运行 pnpm tauri dev 或安装桌面版。";

/** 新增账户的入参 */
export interface AddMailboxInput {
  provider: string;
  email: string;
  password: string;
  label?: string;
  imapHost?: string;
  imapPort?: number;
  smtpHost?: string;
  smtpPort?: number;
}

export interface SendMailInput {
  accountId: string;
  to: string[];
  cc?: string[];
  inReplyTo?: string | null;
  subject: string;
  body: string;
}

function requireDesktop(): void {
  if (!realMailSupported()) throw new Error(NOT_DESKTOP_MSG);
}

export const realMail = {
  /** 服务商预设列表 */
  async providers(): Promise<ProviderPreset[]> {
    requireDesktop();
    return invoke<ProviderPreset[]>("mail_providers", {});
  },

  /** 根据邮箱地址自动匹配服务商 id */
  async guessProvider(email: string): Promise<string> {
    requireDesktop();
    return invoke<string>("mail_guess_provider", { email });
  },

  /** 测试连通性：登录 IMAP 读文件夹列表，不写入任何数据（不需要登录会话） */
  async testConnection(input: AddMailboxInput): Promise<string[]> {
    requireDesktop();
    return invoke<string[]>("mail_test_connection", { input: toRustInput(input) });
  },

  /** 添加（或更新）邮箱账户，成功后授权码加密存入数据库 */
  async addAccount(input: AddMailboxInput, token: string): Promise<RealMailbox> {
    requireDesktop();
    return invoke<RealMailbox>("mail_add_account", { input: toRustInput(input), token });
  },

  /** 列出已接入的真实邮箱账户 */
  async listAccounts(token: string): Promise<RealMailbox[]> {
    requireDesktop();
    return invoke<RealMailbox[]>("mail_list_accounts", { token });
  },

  /** 移除账户 */
  async removeAccount(accountId: string, token: string): Promise<void> {
    requireDesktop();
    await invoke<void>("mail_remove_account", { accountId, token });
  },

  /** 同步单个账户：拉取新邮件 */
  async syncAccount(
    accountId: string,
    token: string,
  ): Promise<{ emails: Email[]; hasMoreHistory: boolean }> {
    requireDesktop();
    const r = await invoke<{ emails: Email[]; hasMoreHistory?: boolean }>("mail_sync_account", {
      accountId,
      token,
    });
    return { emails: r.emails ?? [], hasMoreHistory: r.hasMoreHistory ?? false };
  },

  /** 同步全部真实账户 */
  async syncAll(
    token: string,
  ): Promise<{
    emails: Email[];
    hasMoreHistory: boolean;
    failures: { accountId: string; error: string }[];
  }> {
    requireDesktop();
    const results = await invoke<
      { accountId: string; emails: Email[]; hasMoreHistory?: boolean; error?: string }[]
    >("mail_sync_all", { token });
    return {
      emails: results.flatMap((r) => r.emails ?? []),
      hasMoreHistory: results.some((r) => r.hasMoreHistory),
      failures: results
        .filter((r) => r.error)
        .map((r) => ({ accountId: r.accountId, error: r.error as string })),
    };
  },

  /** 回写已读 / 星标到邮件服务器 */
  async setFlags(
    email: Email,
    patch: { read?: boolean; starred?: boolean },
    token: string,
  ): Promise<void> {
    requireDesktop();
    const { accountId, uid, remoteName } = locator(email);
    await invoke<void>("mail_set_flags", {
      token,
      accountId,
      remoteName,
      uid,
      read: patch.read ?? null,
      starred: patch.starred ?? null,
    });
  },

  /** 归档：移动到服务器归档文件夹 */
  async archive(email: Email, token: string): Promise<void> {
    requireDesktop();
    const { accountId, uid, remoteName } = locator(email);
    await invoke<void>("mail_archive", { token, accountId, remoteName, uid });
  },

  /** 删除：移动到服务器垃圾箱 */
  async trash(email: Email, token: string): Promise<void> {
    requireDesktop();
    const { accountId, uid, remoteName } = locator(email);
    await invoke<void>("mail_trash", { token, accountId, remoteName, uid });
  },

  /** 通过 SMTP 真实发信 */
  async send(input: SendMailInput, token: string): Promise<string> {
    requireDesktop();
    return invoke<string>("mail_send", { input, token });
  },
};

/** 取出回写服务器状态所需的定位信息，缺失时给出明确错误 */
function locator(email: Email): { accountId: string; uid: number; remoteName: string } {
  if (email.uid === undefined || !email.remoteName) {
    throw new Error("这封邮件缺少服务器定位信息，无法同步状态（请重新同步邮箱）");
  }
  return { accountId: email.accountId, uid: email.uid, remoteName: email.remoteName };
}

/** 前端字段转成 Rust 期望的 camelCase 结构（Tauri 会自动做这个映射，这里仅去掉 undefined） */
function toRustInput(input: AddMailboxInput): Record<string, unknown> {
  return {
    provider: input.provider,
    email: input.email,
    password: input.password,
    label: input.label ?? "",
    imapHost: input.imapHost ?? "",
    imapPort: input.imapPort ?? 0,
    smtpHost: input.smtpHost ?? "",
    smtpPort: input.smtpPort ?? 0,
  };
}
