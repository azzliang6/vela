export type FolderId = "inbox" | "starred" | "sent" | "drafts" | "archive" | "trash";

export type SmartCategory =
  | "important"
  | "finance"
  | "travel"
  | "social"
  | "newsletter"
  | "notification"
  | "spam";

export type Provider = "gmail" | "outlook" | "qq" | "163";

export interface Account {
  id: string;
  name: string;
  email: string;
  provider: Provider;
  /** 头像底色 */
  color: string;
  /** true = 真实邮箱账户（走 IMAP/SMTP），false/未定义 = 演示账户 */
  real?: boolean;
}

/** 已接入的真实邮箱账户（连接参数来自本地数据库，不含授权码） */
export interface RealMailbox {
  /** 账户 id，等于邮箱地址 */
  id: string;
  email: string;
  provider: string;
  label: string;
  imapHost: string;
  imapPort: number;
  smtpHost: string;
  smtpPort: number;
}

/** 服务商预设：选中后自动填好服务器地址 */
export interface ProviderPreset {
  id: string;
  label: string;
  imapHost: string;
  imapPort: number;
  smtpHost: string;
  smtpPort: number;
  /** 如何申请授权码的提示 */
  hint: string;
  /** 该服务商使用的邮箱域名，用于自动匹配 */
  domains: string[];
}

export interface Contact {
  name: string;
  email: string;
}

export type AttachmentKind = "pdf" | "sheet" | "doc" | "image" | "zip" | "code";

export interface Attachment {
  id: string;
  name: string;
  sizeKb: number;
  kind: AttachmentKind;
}

export interface Email {
  id: string;
  accountId: string;
  folder: Exclude<FolderId, "starred">;
  from: Contact;
  to: Contact[];
  cc?: Contact[];
  subject: string;
  /** 纯文本正文，段落间以空行分隔 */
  body: string;
  /** HTML 正文（邮件自带 HTML 版本时保留；沙箱 iframe 渲染，无则退回纯文本） */
  htmlBody?: string;
  /** ISO 时间 */
  date: string;
  read: boolean;
  starred: boolean;
  category: SmartCategory | null;
  /** AI 一句话摘要 */
  aiSummary?: string;
  /** AI 判断的重要性理由（仅重要邮件） */
  aiWhy?: string;
  attachments: Attachment[];
  /** 会话内邮件数 */
  threadCount: number;

  /* ———— 以下字段仅真实邮箱邮件有值，用于把状态回写到邮件服务器 ———— */

  /** IMAP UID：在同一文件夹内唯一且稳定 */
  uid?: number;
  /** 该邮件在服务器上的文件夹名（如 "INBOX"、"Sent Messages"） */
  remoteName?: string;
  serverFolder?: "inbox" | "sent";
  /** RFC5322 Message-ID，回复时用于串起会话 */
  messageId?: string;
  /** true = 来自真实邮箱服务器 */
  real?: boolean;
}

export type ComposeMode = "new" | "reply" | "replyAll" | "forward";

/** 列表密度：舒适 / 紧凑 / 单行 */
export type Density = "cozy" | "compact" | "single";

export interface ComposeDraft {
  mode: ComposeMode;
  accountId: string;
  to: string;
  cc: string;
  subject: string;
  body: string;
}

export interface Toast {
  id: number;
  text: string;
  tone: "default" | "success" | "warn";
  /** 展示时长 ms，默认 2700；可撤销操作给 5000 */
  ttl?: number;
  /** 附加动作（如「撤销」） */
  action?: { label: string; run: () => void };
}

export const FOLDERS: { id: FolderId; label: string }[] = [
  { id: "inbox", label: "收件箱" },
  { id: "starred", label: "星标" },
  { id: "sent", label: "已发送" },
  { id: "drafts", label: "草稿箱" },
  { id: "archive", label: "归档" },
  { id: "trash", label: "垃圾箱" },
];

export const CATEGORY_META: Record<
  SmartCategory,
  { label: string; color: string }
> = {
  important: { label: "重要", color: "#D9480F" },
  finance: { label: "财务", color: "#116A7B" },
  travel: { label: "出行", color: "#E08A00" },
  social: { label: "社交", color: "#A8375F" },
  newsletter: { label: "订阅", color: "#4A6FA5" },
  notification: { label: "通知", color: "#6E675C" },
  spam: { label: "垃圾", color: "#A33E3E" },
};

export const AVATAR_PALETTE = [
  "#0D7A6F",
  "#B4472C",
  "#4A6FA5",
  "#7A5CA8",
  "#E0930F",
  "#A8375F",
  "#3D7A4E",
  "#D97706",
  "#2F6B8C",
  "#6E4A3A",
];

/** 由发件人名字稳定地取一个头像色 */
export function avatarColor(seed: string): string {
  let h = 0;
  for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) >>> 0;
  return AVATAR_PALETTE[h % AVATAR_PALETTE.length];
}

/** 姓名首字符：拉丁字母大写，中文取第一个字 */
export function initialOf(name: string): string {
  const ch = name.trim().charAt(0);
  return /[a-z]/i.test(ch) ? ch.toUpperCase() : ch;
}
