/**
 * 桌面通知服务：同步发现新到邮件时通过系统通知中心提醒
 * （Windows 通知中心 / macOS / Linux 均由 tauri-plugin-notification 适配）。
 *
 * - 浏览器开发环境下所有操作自动 no-op（动态引入插件，不打进无 Tauri 的包路径）
 * - 开关持久化在 localStorage，默认开启，可在「设置 · 邮件同步」里切换
 * - 通知只在同步当场发出：Rust 端只给「增量拉取的收件箱未读」打 isNewArrival 标记，
 *   首次接入 / 历史回填 / 已发送 / 在其它设备已读过的邮件都不会触发
 */

import { isDesktop } from "./realMailService";
import type { Email } from "../types";

const LS_KEY = "vela:desktop-notifications";

/** 桌面通知是否开启（默认开） */
export function notificationsEnabled(): boolean {
  return localStorage.getItem(LS_KEY) !== "0";
}

/** 开 / 关桌面通知（立即持久化） */
export function setNotificationsEnabled(on: boolean): void {
  localStorage.setItem(LS_KEY, on ? "1" : "0");
}

/** 动态引入插件模块，避免浏览器环境在打包期解析原生模块 */
async function plugin() {
  return import("@tauri-apps/plugin-notification");
}

/** 确保系统通知权限已授予；返回当前是否可以发通知 */
export async function ensureNotificationPermission(): Promise<boolean> {
  if (!isDesktop()) return false;
  try {
    const p = await plugin();
    let granted = await p.isPermissionGranted();
    if (!granted) granted = (await p.requestPermission()) === "granted";
    return granted;
  } catch (e) {
    console.warn("[vela] 检查桌面通知权限失败：", e);
    return false;
  }
}

/** 一行摘要：发件人 + 主题；多账户时带上账户邮箱以便区分 */
function brief(e: Email, withAccount: boolean): string {
  const subject = e.subject || "（无主题）";
  return withAccount
    ? `${e.accountId} · ${e.from.name}：${subject}`
    : `${e.from.name}：${subject}`;
}

/**
 * 同步发现新到邮件时弹系统通知。
 *
 * 多封时聚合成一条（列出前 3 封摘要），避免一次同步弹一串通知刷屏。
 * 任何失败都静默降级——通知是锦上添花，不能影响同步流程。
 */
export async function notifyNewMail(arrivals: Email[]): Promise<void> {
  if (!arrivals.length || !isDesktop() || !notificationsEnabled()) return;
  try {
    if (!(await ensureNotificationPermission())) return;
    const p = await plugin();
    const withAccount = new Set(arrivals.map((e) => e.accountId)).size > 1;
    if (arrivals.length === 1) {
      p.sendNotification({
        title: "Vela 轻帆 · 新邮件",
        body: brief(arrivals[0], withAccount),
      });
      return;
    }
    const preview = arrivals
      .slice(0, 3)
      .map((e) => brief(e, withAccount))
      .join("\n");
    const more = arrivals.length > 3 ? `\n…还有 ${arrivals.length - 3} 封` : "";
    p.sendNotification({
      title: `Vela 轻帆 · 收到 ${arrivals.length} 封新邮件`,
      body: `${preview}${more}`,
    });
  } catch (e) {
    console.warn("[vela] 发送桌面通知失败：", e);
  }
}

/** 设置面板里的「发送测试通知」；返回是否真的弹出（权限被拒时返回 false） */
export async function sendTestNotification(): Promise<boolean> {
  if (!isDesktop()) return false;
  try {
    if (!(await ensureNotificationPermission())) return false;
    const p = await plugin();
    p.sendNotification({
      title: "Vela 轻帆",
      body: "测试通知 —— 来新邮件时会这样提醒你",
    });
    return true;
  } catch (e) {
    console.warn("[vela] 发送测试通知失败：", e);
    return false;
  }
}
