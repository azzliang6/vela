import type { Email, FolderId, SmartCategory } from "../types";

export interface FilterOptions {
  folder: FolderId;
  /** "all" 表示全部账户的统一收件箱 */
  accountId: string | "all";
  category: SmartCategory | null;
  search: string;
}

/** 按当前视图条件过滤并按时间倒序排列 */
export function filterEmails(emails: Email[], opts: FilterOptions): Email[] {
  const q = opts.search.trim().toLowerCase();
  return emails
    .filter((e) => {
      if (opts.folder === "starred") {
        if (!e.starred || e.folder === "trash") return false;
      } else if (e.folder !== opts.folder) {
        return false;
      }
      if (opts.accountId !== "all" && e.accountId !== opts.accountId) return false;
      if (opts.category && e.category !== opts.category) return false;
      if (q) {
        const hay = [
          e.subject,
          e.from.name,
          e.from.email,
          ...e.to.map((t) => `${t.name} ${t.email}`),
          e.body,
        ]
          .join("\n")
          .toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    })
    .sort((a, b) => +new Date(b.date) - +new Date(a.date));
}

const WEEKDAYS = ["日", "一", "二", "三", "四", "五", "六"];

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** 列表用相对时间：今天 14:32 / 昨天 / 9月8日 / 2025年12月3日 */
export function formatRelative(iso: string): string {
  const d = new Date(iso);
  const now = new Date();
  const sameYear = d.getFullYear() === now.getFullYear();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const t = d.getTime();
  if (t >= startOfToday) return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  if (t >= startOfToday - 86400_000) return "昨天";
  if (sameYear) return `${d.getMonth() + 1}月${d.getDate()}日`;
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日`;
}

/** 阅读窗格用完整时间 */
export function formatFull(iso: string): string {
  const d = new Date(iso);
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日 周${WEEKDAYS[d.getDay()]} ${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`;
}

export function formatSize(kb: number): string {
  return kb >= 1024 ? `${(kb / 1024).toFixed(1)} MB` : `${kb} KB`;
}

/** 预览摘要行：正文去空白拼接，截断 */
export function previewOf(email: Email): string {
  return email.body.replace(/\s+/g, " ").trim().slice(0, 90);
}

/** 姓名的显示形式：有名字用名字，否则用邮箱前缀 */
export function displayName(email: string, name?: string): string {
  if (name && name.trim()) return name;
  return email.split("@")[0];
}
