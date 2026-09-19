import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  type Dispatch,
  type ReactNode,
} from "react";
import type {
  Account,
  ComposeDraft,
  ComposeMode,
  Density,
  Email,
  FolderId,
  RealMailbox,
  SmartCategory,
  Toast,
} from "../types";
import { avatarColor } from "../types";
import { ACCOUNTS, MOCK_EMAILS } from "../data/mockData";
import { filterEmails } from "../services/mailService";
import { analyzeEmail, classifyRules } from "../services/classifyService";
import { deleteEmails, getEmails, isDesktop, syncEmails, type ServerEmail } from "../services/api";
import { realMail, realMailSupported } from "../services/realMailService";
import { aiConfigured, analyzeWithAi, loadAiSettings, type AiSettings as AiSettingsType } from "../services/aiService";
import { errText } from "../services/errText";

export type Theme = "light" | "dark";

/** null = 未登录（显示登录页）；guest = 游客模式（localStorage）；user = PG 账户 */
/** 本地单用户:桌面/手机恒为 user(无账号体系);浏览器开发模式为 guest 演示 */
export type AuthState =
  | { mode: "user"; username: string; token: string }
  | { mode: "guest" }
  | null;

interface State {
  emails: Email[];
  accountId: string | "all";
  folder: FolderId;
  category: SmartCategory | null;
  selectedId: string | null;
  checkedIds: string[];
  density: Density;
  search: string;
  compose: ComposeDraft | null;
  composeDraftId: string | null;
  paletteOpen: boolean;
  theme: Theme;
  toasts: Toast[];
  auth: AuthState;
  booted: boolean;
  /** 已接入的真实邮箱账户 */
  realMailboxes: RealMailbox[];
  /** 是否正在同步真实邮箱 */
  syncing: boolean;
  /** 接入真实邮箱的向导是否打开 */
  addMailboxOpen: boolean;
  /** AI 设置面板是否打开 */
  aiSettingsOpen: boolean;
  /** 正在等待 LLM 返回的邮件 id */
  aiLoadingId: string | null;
  /** 后台自动检查新邮件的间隔（分钟）；0 = 仅手动同步 */
  syncIntervalMin: number;
  /** 上次成功检查新邮件的时间戳；null = 本次会话还没同步过 */
  lastSyncAt: number | null;
}

type Action =
  | { type: "setAuth"; auth: AuthState; emails: Email[] }
  | { type: "bootDone" }
  | { type: "select"; id: string | null }
  | { type: "star"; id: string }
  | { type: "read"; id: string; read: boolean }
  | { type: "readMany"; ids: string[]; read: boolean }
  | { type: "starMany"; ids: string[]; starred: boolean }
  | { type: "move"; id: string; folder: Email["folder"]; nextSelectedId: string | null }
  | {
      type: "moveMany";
      ids: string[];
      folder: Email["folder"];
      nextSelectedId: string | null;
    }
  | { type: "restoreMany"; entries: { id: string; folder: Email["folder"] }[] }
  | { type: "reinsert"; emails: Email[] }
  | { type: "analyze"; id: string }
  | { type: "emptyTrash" }
  | { type: "checkToggle"; id: string }
  | { type: "checksSet"; ids: string[]; mode: "replace" | "union" }
  | { type: "checksClear" }
  | { type: "density"; density: Density }
  | { type: "view"; accountId?: string | "all"; folder?: FolderId; category?: SmartCategory | null }
  | { type: "search"; q: string }
  | { type: "palette"; open: boolean }
  | { type: "theme"; theme: Theme }
  | { type: "compose"; draft: ComposeDraft | null; draftId?: string | null }
  | { type: "composeField"; patch: Partial<ComposeDraft> }
  | { type: "sent"; email: Email; draftId: string | null }
  | { type: "draftSaved"; email: Email; replaceId: string | null }
  | { type: "toastPush"; toast: Toast }
  | { type: "toastPop"; id: number }
  | { type: "realMailboxes"; mailboxes: RealMailbox[] }
  | { type: "syncing"; on: boolean }
  | { type: "addMailbox"; open: boolean }
  | { type: "aiSettings"; open: boolean }
  | { type: "syncInterval"; min: number }
  | { type: "syncedAt"; ts: number }
  | { type: "aiLoading"; id: string | null }
  /** LLM 分析结果落库（覆盖旧值，重新分析也走这里） */
  | { type: "applyAi"; id: string; category: SmartCategory | null; summary: string; why?: string }
  /** 移除某个账户及其全部邮件 */
  | { type: "removeAccountEmails"; accountId: string }
  /** 合并从服务器拉回的真实邮件（按 id 去重，新邮件插在前面） */
  | { type: "mergeReal"; emails: Email[] }
  /** 服务端对账：剔除在服务器上已被删除/归档的本地陈旧副本 */
  | { type: "pruneReal"; entries: { accountId: string; folder: Email["folder"]; uids: Set<number> }[] };

const LS_THEME = "vela:theme";
const LS_PATCHES = "vela:patches-v1";
const LS_USER = "vela:user-emails-v1";
const LS_DENSITY = "vela:density";
const LS_GUEST = "vela:guest";
const LS_SYNC_INTERVAL = "vela:sync-interval-min";

interface Patch {
  read?: boolean;
  starred?: boolean;
  folder?: Email["folder"];
  deleted?: boolean;
}

function loadJSON<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}

const MOCK_BY_ID = new Map(MOCK_EMAILS.map((m) => [m.id, m]));

/** 游客模式：种子邮件 + localStorage 补丁 + 用户自建邮件 */
function hydrateLocalEmails(): Email[] {
  const patches = loadJSON<Record<string, Patch>>(LS_PATCHES, {});
  const users = loadJSON<Email[]>(LS_USER, []).filter((e) => e && e.id && !MOCK_BY_ID.has(e.id));
  const seeds = MOCK_EMAILS.flatMap((e) => {
    const p = patches[e.id];
    if (p?.deleted) return [];
    return [{ ...e, ...(p ?? {}) } as Email];
  });
  return [...seeds, ...users];
}

const DENSITIES: Density[] = ["cozy", "compact", "single"];

function initialDensity(): Density {
  const d = localStorage.getItem(LS_DENSITY) as Density | null;
  return d && DENSITIES.includes(d) ? d : "cozy";
}

function initialTheme(): Theme {
  try {
    const t = localStorage.getItem(LS_THEME);
    if (t === "light" || t === "dark") return t;
  } catch {
    /* ignore */
  }
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function initialState(): State {
  return {
    emails: [],
    accountId: "all",
    folder: "inbox",
    category: null,
    selectedId: null,
    checkedIds: [],
    density: initialDensity(),
    search: "",
    compose: null,
    composeDraftId: null,
    paletteOpen: false,
    theme: initialTheme(),
    toasts: [],
    auth: null,
    booted: false,
    realMailboxes: [],
    syncing: false,
    addMailboxOpen: false,
    aiSettingsOpen: false,
    aiLoadingId: null,
    syncIntervalMin: initialSyncInterval(),
    lastSyncAt: null,
  };
}

/** 后台检查新邮件的间隔（分钟），0 = 仅手动同步；默认 1 分钟 */
function initialSyncInterval(): number {
  try {
    const raw = localStorage.getItem(LS_SYNC_INTERVAL);
    const n = raw === null ? NaN : Number(raw);
    if (Number.isInteger(n) && n >= 0 && n <= 1440) return n;
  } catch {
    /* ignore */
  }
  return 1;
}

function toggleIn(arr: string[], id: string): string[] {
  return arr.includes(id) ? arr.filter((x) => x !== id) : [...arr, id];
}

/** 服务端结构 -> 前端 Email（folder/category 枚举收紧，null 归一为 undefined） */
function fromServer(e: ServerEmail): Email {
  // 数据库 TEXT 字段拒收 \0 字节：混进来的邮件一旦带 NUL 会让整批同步 500，
  // 在进入本地状态的唯一入口统一清洗掉
  const clean = (s: string) => s.replaceAll("\u0000", "");
  return {
    ...e,
    subject: clean(e.subject ?? ""),
    body: clean(e.body ?? ""),
    htmlBody: e.htmlBody ? clean(e.htmlBody) : undefined,
    folder: e.folder as Email["folder"],
    category: e.category as SmartCategory | null,
    cc: e.cc ?? undefined,
    attachments: (e.attachments ?? []) as Email["attachments"],
  };
}

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "setAuth":
      return {
        ...state,
        auth: action.auth,
        emails: action.emails,
        // 进入新会话时重置视图状态
        selectedId: null,
        checkedIds: [],
        compose: null,
        composeDraftId: null,
        search: "",
        category: null,
        folder: "inbox",
        accountId: "all",
        booted: true,
      };
    case "bootDone":
      return { ...state, booted: true };
    case "select":
      return {
        ...state,
        selectedId: action.id,
        emails: action.id
          ? state.emails.map((e) => (e.id === action.id ? { ...e, read: true } : e))
          : state.emails,
      };
    case "star":
      return {
        ...state,
        emails: state.emails.map((e) => (e.id === action.id ? { ...e, starred: !e.starred } : e)),
      };
    case "read":
      return {
        ...state,
        emails: state.emails.map((e) => (e.id === action.id ? { ...e, read: action.read } : e)),
      };
    case "readMany": {
      const set = new Set(action.ids);
      return {
        ...state,
        emails: state.emails.map((e) => (set.has(e.id) ? { ...e, read: action.read } : e)),
      };
    }
    case "starMany": {
      const set = new Set(action.ids);
      return {
        ...state,
        emails: state.emails.map((e) => (set.has(e.id) ? { ...e, starred: action.starred } : e)),
      };
    }
    case "move":
      return {
        ...state,
        selectedId: action.nextSelectedId,
        checkedIds: state.checkedIds.filter((id) => id !== action.id),
        emails: state.emails.map((e) =>
          e.id === action.id ? { ...e, folder: action.folder } : e,
        ),
      };
    case "moveMany": {
      const set = new Set(action.ids);
      return {
        ...state,
        selectedId: state.selectedId && set.has(state.selectedId) ? action.nextSelectedId : state.selectedId,
        checkedIds: state.checkedIds.filter((id) => !set.has(id)),
        emails: state.emails.map((e) => (set.has(e.id) ? { ...e, folder: action.folder } : e)),
      };
    }
    case "restoreMany": {
      const map = new Map(action.entries.map((en) => [en.id, en.folder]));
      return {
        ...state,
        emails: state.emails.map((e) =>
          map.has(e.id) ? { ...e, folder: map.get(e.id)! } : e,
        ),
      };
    }
    case "reinsert": {
      const known = new Set(state.emails.map((e) => e.id));
      return { ...state, emails: [...action.emails.filter((e) => !known.has(e.id)), ...state.emails] };
    }
    case "analyze":
      return {
        ...state,
        emails: state.emails.map((e) => {
          if (e.id !== action.id || e.category) return e;
          const r = analyzeEmail(e);
          return r ? { ...e, category: r.category, aiSummary: r.summary, aiWhy: r.why } : e;
        }),
      };
    case "emptyTrash":
      return {
        ...state,
        selectedId: null,
        checkedIds: [],
        emails: state.emails.filter((e) => e.folder !== "trash"),
      };
    case "checkToggle":
      return { ...state, checkedIds: toggleIn(state.checkedIds, action.id) };
    case "checksSet":
      return {
        ...state,
        checkedIds:
          action.mode === "replace"
            ? action.ids
            : Array.from(new Set([...state.checkedIds, ...action.ids])),
      };
    case "checksClear":
      return { ...state, checkedIds: [] };
    case "density":
      return { ...state, density: action.density };
    case "view":
      return {
        ...state,
        accountId: action.accountId ?? state.accountId,
        folder: action.folder ?? state.folder,
        category: action.category !== undefined ? action.category : state.category,
        selectedId: null,
        checkedIds: [],
      };
    case "search":
      return { ...state, search: action.q, selectedId: null };
    case "palette":
      return { ...state, paletteOpen: action.open };
    case "theme":
      return { ...state, theme: action.theme };
    case "compose":
      return {
        ...state,
        compose: action.draft,
        composeDraftId: action.draftId ?? null,
      };
    case "composeField":
      return state.compose
        ? { ...state, compose: { ...state.compose, ...action.patch } }
        : state;
    case "sent":
      return {
        ...state,
        compose: null,
        composeDraftId: null,
        emails: [
          action.email,
          ...state.emails.filter((e) => e.id !== action.draftId),
        ],
        toasts: [
          ...state.toasts,
          { id: Date.now(), text: `已发送至 ${action.email.to[0]?.name ?? "收件人"}`, tone: "success" },
        ],
      };
    case "draftSaved":
      return {
        ...state,
        compose: null,
        composeDraftId: null,
        emails: action.replaceId
          ? state.emails.map((e) => (e.id === action.replaceId ? action.email : e))
          : [action.email, ...state.emails],
        toasts: [
          ...state.toasts,
          { id: Date.now(), text: "草稿已保存", tone: "default" },
        ],
      };
    case "toastPush":
      return { ...state, toasts: [...state.toasts, action.toast] };
    case "toastPop":
      return { ...state, toasts: state.toasts.filter((t) => t.id !== action.id) };
    case "realMailboxes":
      return { ...state, realMailboxes: action.mailboxes };
    case "syncing":
      return { ...state, syncing: action.on };
    case "addMailbox":
      return { ...state, addMailboxOpen: action.open };
    case "aiSettings":
      return { ...state, aiSettingsOpen: action.open };
    case "syncInterval":
      return { ...state, syncIntervalMin: action.min };
    case "syncedAt":
      return { ...state, lastSyncAt: action.ts };
    case "aiLoading":
      return { ...state, aiLoadingId: action.id };
    case "applyAi":
      return {
        ...state,
        emails: state.emails.map((e) =>
          e.id === action.id
            ? {
                ...e,
                category: action.category ?? e.category,
                aiSummary: action.summary || e.aiSummary,
                aiWhy: action.why,
              }
            : e,
        ),
      };
    case "removeAccountEmails":
      return {
        ...state,
        emails: state.emails.filter((e) => e.accountId !== action.accountId),
        accountId: state.accountId === action.accountId ? "all" : state.accountId,
        selectedId:
          state.selectedId &&
          state.emails.find((e) => e.id === state.selectedId)?.accountId === action.accountId
            ? null
            : state.selectedId,
      };
    case "mergeReal": {
      // 按 id 去重：已存在的保留本地状态（已读/星标可能是用户刚改的），
      // 服务器上新增的邮件插到列表前面
      const known = new Set(state.emails.map((e) => e.id));
      const fresh = action.emails.filter((e) => !known.has(e.id));
      if (!fresh.length) return state;
      return { ...state, emails: [...fresh, ...state.emails] };
    }
    case "pruneReal": {
      // 只动真实邮件（有 uid 的）；本地副本在服务器上已不存在（网页端删了/归档了）就移除
      let changed = false;
      const emails = state.emails.filter((e) => {
        if (!e.real || typeof e.uid !== "number") return true;
        const entry = action.entries.find((p) => p.accountId === e.accountId && p.folder === e.folder);
        if (!entry) return true;
        if (entry.uids.has(e.uid)) return true;
        changed = true;
        return false;
      });
      if (!changed) return state;
      return { ...state, emails };
    }
    default:
      return state;
  }
}

function neighborAfterRemove(state: State, removeId: string): string | null {
  const visible = filterEmails(state.emails, {
    folder: state.folder,
    accountId: state.accountId,
    category: state.category,
    search: state.search,
  });
  const idx = visible.findIndex((e) => e.id === removeId);
  if (idx === -1) return state.selectedId !== removeId ? state.selectedId : null;
  const rest = visible.filter((e) => e.id !== removeId);
  return rest[idx]?.id ?? rest[idx - 1]?.id ?? null;
}

function parseContacts(raw: string): { name: string; email: string }[] {
  return raw
    .split(/[;；,，]/)
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => {
      const m = s.match(/^(.*)<(.+)>$/);
      if (m) return { name: m[1].trim() || m[2], email: m[2].trim() };
      return { name: s, email: s };
    });
}


export interface MailContextValue {
  state: State;
  dispatch: Dispatch<Action>;
  accounts: Account[];
  visibleEmails: Email[];
  selectedEmail: Email | null;
  unreadByAccount: Record<string, number>;
  unreadAll: number;
  actions: {
    enterGuest: () => void;
    select: (id: string | null) => void;
    toggleStar: (id: string) => void;
    markRead: (id: string, read: boolean) => void;
    archive: (id: string) => void;
    trash: (id: string) => void;
    restore: (id: string) => void;
    emptyTrash: () => void;
    setView: (v: {
      accountId?: string | "all";
      folder?: FolderId;
      category?: SmartCategory | null;
    }) => void;
    setSearch: (q: string) => void;
    setTheme: (t: Theme) => void;
    toggleTheme: () => void;
    setDensity: (d: Density) => void;
    toggleCheck: (id: string) => void;
    checksSet: (ids: string[], mode?: "replace" | "union") => void;
    checksClear: () => void;
    batchRead: (ids: string[], read: boolean) => void;
    batchStar: (ids: string[], starred: boolean) => void;
    batchArchive: (ids: string[]) => void;
    batchTrash: (ids: string[]) => void;
    openPalette: () => void;
    closePalette: () => void;
    newCompose: (accountId?: string) => void;
    openDraft: (email: Email) => void;
    replyTo: (email: Email, all?: boolean) => void;
    forward: (email: Email) => void;
    closeCompose: (save?: boolean) => void;
    updateCompose: (patch: Partial<ComposeDraft>) => void;
    sendCompose: () => void;
    analyze: (id: string) => void;
    /** 批量分析所有未分析邮件（不限 3 天），完成后提示 */
    analyzeAll: () => void;
    toast: (text: string, tone?: Toast["tone"]) => void;
    dismissToast: (id: number) => void;
    /* ———— 真实邮箱 ———— */
    /** 载入已接入的真实邮箱账户列表 */
    loadRealMailboxes: () => Promise<void>;
    /** 同步全部真实邮箱（拉取新邮件）；auto=true 为后台静默模式 */
    syncRealMail: (silent?: boolean, auto?: boolean) => Promise<void>;
    /** 打开 / 关闭「接入真实邮箱」向导 */
    openAddMailbox: () => void;
    closeAddMailbox: () => void;
    /** 打开 / 关闭「AI 设置」面板 */
    openAiSettings: () => void;
    closeAiSettings: () => void;
    /** 移除一个真实邮箱账户 */
    removeRealMailbox: (accountId: string) => Promise<void>;
  };
}

const MailContext = createContext<MailContextValue | null>(null);

export function MailProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, undefined, initialState);

  /** 从本地 SQLite 恢复会话（桌面 / 手机） */
  const loadLocalSession = async () => {
    const emails = await getEmails();
    // 过滤历史版本播种过、已落库的演示邮件（mock id 均为 m001…）
    const list = emails.map(fromServer).filter((e) => !MOCK_BY_ID.has(e.id));
    dispatch({ type: "setAuth", auth: { mode: "user", username: "本地", token: "" }, emails: list });
  };

  /** 载入真实邮箱账户列表（仅桌面版可用，失败不阻断其它功能） */
  const loadRealMailboxes = async () => {
    if (!realMailSupported() || state.auth?.mode !== "user") return;
    try {
      const list = await realMail.listAccounts();
      dispatch({ type: "realMailboxes", mailboxes: list });
    } catch {
      /* 数据库未就绪等情况：静默失败，不影响演示功能 */
    }
  };

  // 启动：桌面/手机直接进入本地会话；浏览器（无本地库）进游客演示
  useEffect(() => {
    (async () => {
      if (isDesktop()) {
        try {
          await loadLocalSession();
          return;
        } catch (e) {
          console.warn("[vela] 本地库读取失败，退回游客演示：", e);
        }
      }
      if (!isDesktop() || localStorage.getItem(LS_GUEST) === "1") {
        dispatch({ type: "setAuth", auth: { mode: "guest" }, emails: hydrateLocalEmails() });
        return;
      }
      dispatch({ type: "bootDone" });
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 进入用户会话后载入真实邮箱账户列表
  useEffect(() => {
    if (state.booted && state.auth?.mode === "user") {
      void loadRealMailboxes();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.booted, state.auth?.mode]);

  // 持久化：主题与密度始终本地
  useEffect(() => {
    try {
      localStorage.setItem(LS_THEME, state.theme);
      localStorage.setItem(LS_DENSITY, state.density);
    } catch {
      /* ignore */
    }
  }, [state.theme, state.density]);

  // 持久化：邮件检查频率
  useEffect(() => {
    try {
      localStorage.setItem(LS_SYNC_INTERVAL, String(state.syncIntervalMin));
    } catch {
      /* ignore */
    }
  }, [state.syncIntervalMin]);

  // 持久化：游客模式写 localStorage（登出状态不得写入，否则会清空游客数据）
  useEffect(() => {
    if (state.auth?.mode !== "guest") return;
    try {
      const patches: Record<string, Patch> = {};
      const liveIds = new Set(state.emails.map((e) => e.id));
      for (const m of MOCK_EMAILS) {
        if (!liveIds.has(m.id)) {
          patches[m.id] = { deleted: true };
          continue;
        }
        const cur = state.emails.find((e) => e.id === m.id)!;
        const p: Patch = {};
        if (cur.read !== m.read) p.read = cur.read;
        if (cur.starred !== m.starred) p.starred = cur.starred;
        if (cur.folder !== m.folder) p.folder = cur.folder;
        if (Object.keys(p).length) patches[m.id] = p;
      }
      localStorage.setItem(LS_PATCHES, JSON.stringify(patches));
      localStorage.setItem(LS_USER, JSON.stringify(state.emails.filter((e) => !MOCK_BY_ID.has(e.id)).slice(0, 200)));
    } catch {
      /* ignore */
    }
  }, [state.emails, state.auth, state.booted]);

  // 持久化：防抖保存到本地 SQLite
  useEffect(() => {
    if (!state.booted || state.auth?.mode !== "user") return;
    const t = window.setTimeout(() => {
      syncEmails(state.emails).catch((err) => {
        dispatch({
          type: "toastPush",
          toast: { id: Date.now(), text: `本地保存失败：${errText(err)}`, tone: "warn" },
        });
      });
    }, 800);
    return () => window.clearTimeout(t);
  }, [state.emails, state.auth, state.booted]);

  useEffect(() => {
    document.documentElement.dataset.theme = state.theme;
  }, [state.theme]);

  const visibleEmails = useMemo(
    () =>
      filterEmails(state.emails, {
        folder: state.folder,
        accountId: state.accountId,
        category: state.category,
        search: state.search,
      }),
    [state.emails, state.folder, state.accountId, state.category, state.search],
  );

  const selectedEmail = useMemo(
    () => state.emails.find((e) => e.id === state.selectedId) ?? null,
    [state.emails, state.selectedId],
  );

  const unreadByAccount = useMemo(() => {
    const map: Record<string, number> = {};
    for (const e of state.emails) {
      if (e.folder === "inbox" && !e.read) map[e.accountId] = (map[e.accountId] ?? 0) + 1;
    }
    return map;
  }, [state.emails]);

  const unreadAll = useMemo(
    () => Object.values(unreadByAccount).reduce((a, b) => a + b, 0),
    [unreadByAccount],
  );

  const actions = useMemo<MailContextValue["actions"]>(() => {
    const notify = (text: string, tone: Toast["tone"], undoEntries?: { id: string; folder: Email["folder"] }[], reinsertEmails?: Email[]) =>
      dispatch({
        type: "toastPush",
        toast: {
          id: Date.now() + Math.random(),
          text,
          tone,
          ttl: undoEntries || reinsertEmails ? 5000 : undefined,
          action: undoEntries
            ? { label: "撤销", run: () => dispatch({ type: "restoreMany", entries: undoEntries }) }
            : reinsertEmails
              ? { label: "撤销", run: () => dispatch({ type: "reinsert", emails: reinsertEmails }) }
              : undefined,
        },
      });

    const moveTo = (ids: string[], folder: Email["folder"], text: string) => {
      const byId = new Map(state.emails.map((e) => [e.id, e]));
      const snapshot = ids
        .map((id) => byId.get(id))
        .filter((e): e is Email => !!e)
        .map((e) => ({ id: e.id, folder: e.folder }));
      const nextSelectedId =
        ids.length === 1 ? neighborAfterRemove(state, ids[0]) : state.selectedId;
      dispatch({ type: "moveMany", ids, folder, nextSelectedId });
      notify(`${text}${ids.length > 1 ? ` ${ids.length} 封` : ""}`, "default", snapshot);
    };

    const quotedBody = (e: Email): string =>
      `\n\n——— 原始邮件 ———\n发件人：${e.from.name} <${e.from.email}>\n时间：${e.date}\n主题：${e.subject}\n\n${e.body}`;

    return {
      enterGuest: () => {
        localStorage.setItem(LS_GUEST, "1");
        dispatch({ type: "setAuth", auth: { mode: "guest" }, emails: hydrateLocalEmails() });
      },
      select: (id) => {
        dispatch({ type: "select", id });
        // 真实邮件：把「已读」回写到服务器（失败仅提示，不回滚本地，避免界面闪烁）
        const target = state.emails.find((e) => e.id === id);
        if (target?.real && !target.read && state.auth?.mode === "user") {
          void realMail
            .setFlags(target, { read: true })
            .catch((err) => notify(`已读状态未能同步到服务器：${errText(err)}`, "warn"));
        }
      },
      toggleStar: (id) => {
        const target = state.emails.find((e) => e.id === id);
        dispatch({ type: "star", id });
        if (target?.real && state.auth?.mode === "user") {
          void realMail
            .setFlags(target, { starred: !target.starred })
            .catch((err) => notify(`星标未能同步到服务器：${errText(err)}`, "warn"));
        }
      },
      markRead: (id, read) => {
        const target = state.emails.find((e) => e.id === id);
        dispatch({ type: "read", id, read });
        if (target?.real && state.auth?.mode === "user") {
          void realMail
            .setFlags(target, { read })
            .catch((err) => notify(`已读状态未能同步到服务器：${errText(err)}`, "warn"));
        }
      },
      archive: (id) => {
        const target = state.emails.find((e) => e.id === id);
        moveTo([id], "archive", "已归档");
        if (target?.real && state.auth?.mode === "user") {
          void realMail
            .archive(target)
            .catch((err) => notify(`服务器归档失败：${errText(err)}`, "warn"));
        }
      },
      trash: (id) => {
        const target = state.emails.find((e) => e.id === id);
        moveTo([id], "trash", "已移入垃圾箱");
        if (target?.real && state.auth?.mode === "user") {
          void realMail
            .trash(target)
            .catch((err) => notify(`服务器删除失败：${errText(err)}`, "warn"));
        }
      },
      restore: (id) =>
        dispatch({ type: "move", id, folder: "inbox", nextSelectedId: null }),
      emptyTrash: () => {
        const removed = state.emails.filter((e) => e.folder === "trash");
        if (!removed.length) return;
        dispatch({ type: "emptyTrash" });
        // 本地库同步删除（撤销只是界面恢复，重新保存会补回）
        void deleteEmails(removed.map((e) => e.id));
        notify(`垃圾箱已清空（${removed.length} 封）`, "success", undefined, removed);
      },
      setView: (v) => dispatch({ type: "view", ...v }),
      setSearch: (q) => dispatch({ type: "search", q }),
      setTheme: (theme) => dispatch({ type: "theme", theme }),
      toggleTheme: () =>
        dispatch({ type: "theme", theme: state.theme === "dark" ? "light" : "dark" }),
      setDensity: (density) => dispatch({ type: "density", density }),
      toggleCheck: (id) => dispatch({ type: "checkToggle", id }),
      checksSet: (ids, mode = "replace") => dispatch({ type: "checksSet", ids, mode }),
      checksClear: () => dispatch({ type: "checksClear" }),
      batchRead: (ids, read) => {
        dispatch({ type: "readMany", ids, read });
        notify(read ? `已标为已读（${ids.length} 封）` : `已标为未读（${ids.length} 封）`, "default");
        // 真实邮件逐封回写（IMAP 没有批量跨文件夹接口，逐封最稳妥）
        if (state.auth?.mode === "user") {
          for (const id of ids) {
            const e = state.emails.find((x) => x.id === id);
            if (e?.real) {
              void realMail.setFlags(e, { read }).catch(() => {
                /* 单封失败不打断整批 */
              });
            }
          }
        }
      },
      batchStar: (ids, starred) => {
        dispatch({ type: "starMany", ids, starred });
        notify(starred ? `已加星标（${ids.length} 封）` : `已去星标（${ids.length} 封）`, "default");
        if (state.auth?.mode === "user") {
          for (const id of ids) {
            const e = state.emails.find((x) => x.id === id);
            if (e?.real) {
              void realMail.setFlags(e, { starred }).catch(() => {
                /* 单封失败不打断整批 */
              });
            }
          }
        }
      },
      batchArchive: (ids) => {
        moveTo(ids, "archive", "已归档");
        if (state.auth?.mode === "user") {
          for (const id of ids) {
            const e = state.emails.find((x) => x.id === id);
            if (e?.real) void realMail.archive(e).catch(() => {});
          }
        }
      },
      batchTrash: (ids) => {
        moveTo(ids, "trash", "已移入垃圾箱");
        if (state.auth?.mode === "user") {
          for (const id of ids) {
            const e = state.emails.find((x) => x.id === id);
            if (e?.real) void realMail.trash(e).catch(() => {});
          }
        }
      },
      openPalette: () => dispatch({ type: "palette", open: true }),
      closePalette: () => dispatch({ type: "palette", open: false }),
      newCompose: (accountId) => {
        // 未指定账户时优先用第一个真实邮箱，没有真实邮箱才退回演示账户
        const fallback = state.realMailboxes[0]?.id ?? ACCOUNTS[0].id;
        const acct = accountId ?? (state.accountId === "all" ? fallback : state.accountId);
        dispatch({
          type: "compose",
          draft: {
            mode: "new" as ComposeMode,
            accountId: acct,
            to: "",
            cc: "",
            subject: "",
            body: "",
          },
        });
      },
      openDraft: (email) =>
        dispatch({
          type: "compose",
          draftId: email.id,
          draft: {
            mode: "new",
            accountId: email.accountId,
            to: email.to.map((t) => t.email).join(", "),
            cc: "",
            subject: email.subject,
            body: email.body,
          },
        }),
      replyTo: (email, all = false) =>
        dispatch({
          type: "compose",
          draft: {
            mode: (all ? "replyAll" : "reply") as ComposeMode,
            accountId: email.accountId,
            to: `${email.from.name} <${email.from.email}>`,
            cc: all && email.cc?.length ? email.cc.map((c) => `${c.name} <${c.email}>`).join(", ") : "",
            subject: email.subject.startsWith("Re:") ? email.subject : `Re: ${email.subject}`,
            body: quotedBody(email),
          },
        }),
      forward: (email) =>
        dispatch({
          type: "compose",
          draft: {
            mode: "forward" as ComposeMode,
            accountId: email.accountId,
            to: "",
            cc: "",
            subject: email.subject.startsWith("Fwd:") ? email.subject : `Fwd: ${email.subject}`,
            body: quotedBody(email),
          },
        }),
      closeCompose: (save = false) => {
        const draft = state.compose;
        if (!draft) return;
        const dirty = draft.to.trim() || draft.subject.trim() || draft.body.trim();
        if (save && dirty) {
          const realBox = state.realMailboxes.find((m) => m.id === draft.accountId);
          const demo = ACCOUNTS.find((a) => a.id === draft.accountId) ?? ACCOUNTS[0];
          const fromName = realBox?.label || realBox?.email || demo.name;
          const fromEmail = realBox?.email ?? demo.email;
          const email: Email = {
            id: state.composeDraftId ?? `draft-${Date.now()}`,
            accountId: draft.accountId,
            folder: "drafts",
            from: { name: fromName, email: fromEmail },
            to: parseContacts(draft.to),
            cc: draft.cc ? parseContacts(draft.cc) : undefined,
            subject: draft.subject || "（无主题）",
            body: draft.body,
            date: new Date().toISOString(),
            read: true,
            starred: false,
            category: null,
            attachments: [],
            threadCount: 1,
          };
          dispatch({ type: "draftSaved", email, replaceId: state.composeDraftId });
          return;
        }
        dispatch({ type: "compose", draft: null });
        if (dirty) dispatch({ type: "toastPush", toast: { id: Date.now(), text: "已舍弃草稿", tone: "default" } });
      },
      updateCompose: (patch) => dispatch({ type: "composeField", patch }),
      sendCompose: () => {
        const draft = state.compose;
        if (!draft) return;
        if (!draft.to.trim()) {
          dispatch({ type: "toastPush", toast: { id: Date.now(), text: "请填写收件人", tone: "warn" } });
          return;
        }

        // 真实邮箱账户：走 SMTP 真实投递
        const realBox = state.realMailboxes.some((m) => m.id === draft.accountId);
        if (realBox && state.auth?.mode === "user") {
          const draftId = state.composeDraftId;
          const quoted = state.emails.find((e) => e.id === state.selectedId);
          const subject = draft.subject || "（无主题）";
          const body = draft.body;
          const to = parseContacts(draft.to);
          const cc = draft.cc ? parseContacts(draft.cc) : undefined;

          actions.toast("正在发送…", "default");
          void realMail
            .send(
              {
                accountId: draft.accountId,
                to: to.map((c) => (c.name && c.name !== c.email ? `${c.name} <${c.email}>` : c.email)),
                cc: cc?.map((c) => (c.name && c.name !== c.email ? `${c.name} <${c.email}>` : c.email)),
                inReplyTo:
                  draft.mode === "reply" || draft.mode === "replyAll"
                    ? (quoted?.messageId ?? null)
                    : null,
                subject,
                body,
              },
            )
            .then(() => {
              // 本地先显示在已发送，服务器端稍后同步会拉到权威副本
              const sent: Email = {
                id: `sent-${Date.now()}`,
                accountId: draft.accountId,
                folder: "sent",
                from: {
                  name:
                    state.realMailboxes.find((m) => m.id === draft.accountId)?.label ??
                    draft.accountId,
                  email: draft.accountId,
                },
                to,
                cc,
                subject,
                body,
                date: new Date().toISOString(),
                read: true,
                starred: false,
                category: null,
                attachments: [],
                threadCount: 1,
              };
              dispatch({ type: "sent", email: sent, draftId });
            })
            .catch((err) => {
              // 发送失败时保留写信窗口与草稿，用户可重试或修改后重发
              dispatch({
                type: "toastPush",
                toast: { id: Date.now(), text: `发送失败：${errText(err)}`, tone: "warn", ttl: 6000 },
              });
            });
          return;
        }

        const account = ACCOUNTS.find((a) => a.id === draft.accountId) ?? ACCOUNTS[0];
        const sent: Email = {
          id: `sent-${Date.now()}`,
          accountId: draft.accountId,
          folder: "sent",
          from: { name: account.name, email: account.email },
          to: parseContacts(draft.to),
          cc: draft.cc ? parseContacts(draft.cc) : undefined,
          subject: draft.subject || "（无主题）",
          body: draft.body,
          date: new Date().toISOString(),
          read: true,
          starred: false,
          category: null,
          attachments: [],
          threadCount: 1,
        };
        const ruleResult = analyzeEmail(sent);
        if (ruleResult) {
          sent.category = ruleResult.category;
          sent.aiSummary = ruleResult.summary;
          sent.aiWhy = ruleResult.why;
        }
        dispatch({ type: "sent", email: sent, draftId: state.composeDraftId });
      },
      analyze: (id) => {
        const target = state.emails.find((e) => e.id === id);
        if (!target || state.aiLoadingId) return;

        // 已配置真实 LLM：优先走 API（结果覆盖旧分析，支持重新分析）
        const settings = loadAiSettings();
        if (aiConfigured(settings)) {
          dispatch({ type: "aiLoading", id });
          notify("AI 正在分析…", "default");
          void analyzeWithAi(target, settings)
            .then((r) => {
              dispatch({ type: "applyAi", id, ...r });
              notify(
                r.category ? "AI 分析完成" : "AI 已返回内容，但未能解析出分类",
                r.category ? "success" : "default",
              );
            })
            .catch((err) => notify(`AI 分析失败：${errText(err)}`, "warn"))
            .finally(() => dispatch({ type: "aiLoading", id: null }));
          return;
        }

        // 未配置 AI：本地规则兜底（离线可用）
        if (target.category) return;
        dispatch({ type: "analyze", id });
        if (classifyRules(target)) {
          notify("本地规则分析完成（可在侧边栏配置真实 AI）", "success");
        } else {
          notify("规则未命中，保持未分类；配置真实 AI 后效果更好", "warn");
        }
      },
      analyzeAll: () => {
        const settings = loadAiSettings();
        if (!aiConfigured(settings)) {
          notify("请先在 AI 设置里配置服务商与 API Key", "warn");
          actions.openAiSettings();
          return;
        }
        if (aiBatchBusyRef.current) {
          notify("已有一批邮件正在分析中", "default");
          return;
        }
        // 全部未分析的收件箱真实邮件（不限时间），按时间新→旧
        const candidates = state.emails
          .filter((e) => e.folder === "inbox" && !e.aiSummary && !e.category && !aiAttemptedRef.current.has(e.id))
          .sort((a, b) => +new Date(b.date) - +new Date(a.date));
        if (!candidates.length) {
          notify("没有待分析的邮件", "default");
          return;
        }
        notify(`开始批量分析 ${candidates.length} 封邮件…`, "default");
        void analyzeBatch(candidates, settings, (done, total) => {
          if (done % 20 === 0 || done === total) {
            notify(`批量分析进度 ${done}/${total}`, "default");
          }
        }).then((r) => {
          if (!r) return;
          if (r.done) {
            notify(`批量分析完成：成功 ${r.done} 封${r.failed ? `，失败 ${r.failed} 封` : ""}`, r.failed ? "default" : "success");
          } else {
            notify(`批量分析失败（${r.failed} 封），请检查 AI 设置或网络`, "warn");
          }
        });
      },
      toast: (text, tone = "default") =>
        dispatch({ type: "toastPush", toast: { id: Date.now(), text, tone } }),
      dismissToast: (id) => dispatch({ type: "toastPop", id }),
      /* ———— 真实邮箱 ———— */
      loadRealMailboxes,
      /** auto = 后台定时同步：完全静默（不弹提示、不转同步图标） */
      syncRealMail: async (silent = false, auto = false) => {
        if (!realMailSupported()) {
          if (!silent) actions.toast("真实邮箱需要在桌面应用中使用", "warn");
          return;
        }
        if (state.auth?.mode !== "user") {
          if (!silent) actions.toast("请先登录后再同步真实邮箱", "warn");
          return;
        }
        if (!state.realMailboxes.length) {
          if (!silent) actions.toast("尚未接入任何真实邮箱", "warn");
          return;
        }
        // 手动同步进行中就不叠加一次自动同步
        if (auto && state.syncing) return;

        if (!auto) dispatch({ type: "syncing", on: true });
        try {
          const { emails: incoming, failures, hasMoreHistory, present } = await realMail.syncAll();

          // 先算真正的新邮件（id 尚未在本地），提示才不会虚报
          const knownIds = new Set(state.emails.map((e) => e.id));
          const fresh = incoming.filter((e) => !knownIds.has(e.id));

          dispatch({ type: "mergeReal", emails: incoming });
          dispatch({ type: "syncedAt", ts: Date.now() });
          // 服务端对账：只对同步成功的账户做（failures 里的账户结果不可信）
          const okAccounts = new Set(
            state.realMailboxes.map((m) => m.id).filter((id) => !failures.some((f) => f.accountId === id)),
          );
          const pruneEntries = present
            .filter((p) => okAccounts.has(p.accountId))
            .map((p) => ({ accountId: p.accountId, folder: p.folder as Email["folder"], uids: new Set(p.uids) }));
          dispatch({ type: "pruneReal", entries: pruneEntries });
          // 本地库同步删除在服务器上已不存在的邮件
          const gone = state.emails.filter((e) => {
            if (!e.real || typeof e.uid !== "number") return false;
            const entry = pruneEntries.find((p) => p.accountId === e.accountId && p.folder === e.folder);
            return entry ? !entry.uids.has(e.uid) : false;
          });
          void deleteEmails(gone.map((e) => e.id));

          if (auto) {
            // 后台同步静默完成；有新邮件时轻提示一声
            if (fresh.length) actions.toast(`已收取 ${fresh.length} 封新邮件`, "success");
            return;
          }
          // 有账户同步失败时必须告知用户，否则会误以为“没有新邮件”
          if (failures.length) {
            const detail = failures
              .map((f) => `${f.accountId}：${f.error}`)
              .join("；");
            dispatch({
              type: "toastPush",
              toast: {
                id: Date.now(),
                text: `${failures.length} 个账户同步失败 —— ${detail}`,
                tone: "warn",
                ttl: 8000,
              },
            });
          } else if (!silent || hasMoreHistory) {
            // 还有更早的历史邮件时，明确告诉用户再点一次可以继续加载
            const base = incoming.length
              ? `已收取 ${incoming.length} 封邮件`
              : "没有新邮件";
            actions.toast(
              hasMoreHistory ? `${base} · 再点一次同步可加载更早的邮件` : base,
              "success",
            );
          }
        } catch (e) {
          if (!auto) {
            dispatch({
              type: "toastPush",
              toast: {
                id: Date.now(),
                text: `同步失败：${errText(e)}`,
                tone: "warn",
                ttl: 6000,
              },
            });
          } else {
            console.warn("[vela] 后台自动同步失败：", e);
          }
        } finally {
          if (!auto) dispatch({ type: "syncing", on: false });
        }
      },
      openAddMailbox: () => dispatch({ type: "addMailbox", open: true }),
      closeAddMailbox: () => dispatch({ type: "addMailbox", open: false }),
      openAiSettings: () => dispatch({ type: "aiSettings", open: true }),
      closeAiSettings: () => dispatch({ type: "aiSettings", open: false }),
      removeRealMailbox: async (accountId) => {
        if (state.auth?.mode !== "user") return;
        try {
          await realMail.removeAccount(accountId);
          await loadRealMailboxes();
          // 一并清掉本地该账户的邮件（含库内记录），避免留下无法操作的孤儿数据
          dispatch({ type: "removeAccountEmails", accountId });
          void deleteEmails(state.emails.filter((e) => e.accountId === accountId).map((e) => e.id));
          actions.toast(`已移除 ${accountId}`, "default");
        } catch (e) {
          actions.toast(
            `移除失败：${e instanceof Error ? e.message : "未知错误"}`,
            "warn",
          );
        }
      },
    };
  }, [state]);

  /* ———— 后台自动同步：新邮件不用等手动点同步 ———— */
  const autoSyncRef = useRef(actions.syncRealMail);
  useEffect(() => {
    autoSyncRef.current = actions.syncRealMail;
  });
  const hasMailboxes = state.realMailboxes.length > 0;
  useEffect(() => {
    if (!state.booted || state.auth?.mode !== "user" || !realMailSupported() || !hasMailboxes) {
      return;
    }
    // 0 = 仅手动同步：不自动检查
    if (state.syncIntervalMin === 0) return;
    // 进入会话立即同步一次，之后按设置的间隔后台静默检查
    void autoSyncRef.current(true, true);
    const ms = Math.max(15_000, state.syncIntervalMin * 60_000);
    const iv = window.setInterval(() => void autoSyncRef.current(true, true), ms);
    return () => window.clearInterval(iv);
  }, [state.booted, state.auth?.mode, hasMailboxes, state.syncIntervalMin]);

  /* ———— AI 批量/自动分析共用：同一时间只跑一批，逐封带间隔 ———— */
  const aiBatchBusyRef = useRef(false);
  /** 本会话内已尝试过自动分析的邮件 id（失败也不重试，避免坏 Key 造成请求风暴） */
  const aiAttemptedRef = useRef<Set<string>>(new Set());
  /** 上次自动分析扫描完成的时间（用于按设置节流） */
  const lastAutoScanRef = useRef(0);

  /** 批量分析一批邮件。返回 {done, failed}；有其它批次在跑时返回 null */
  const analyzeBatch = async (
    candidates: Email[],
    settings: AiSettingsType,
    onProgress?: (done: number, total: number) => void,
  ): Promise<{ done: number; failed: number } | null> => {
    if (aiBatchBusyRef.current || !candidates.length) return null;
    aiBatchBusyRef.current = true;
    let done = 0;
    let failed = 0;
    try {
      for (const email of candidates) {
        aiAttemptedRef.current.add(email.id);
        // 逐封点亮加载态：列表/阅读页据此显示转圈并禁用重复触发
        dispatch({ type: "aiLoading", id: email.id });
        try {
          const r = await analyzeWithAi(email, settings);
          dispatch({ type: "applyAi", id: email.id, ...r });
          done += 1;
        } catch {
          failed += 1;
          // 连续失败说明 Key/网络有问题，直接中止整批，避免逐封都超时一遍
          if (failed >= 3 && done === 0) break;
        }
        dispatch({ type: "aiLoading", id: null });
        onProgress?.(done + failed, candidates.length);
        await new Promise((res) => window.setTimeout(res, 200));
      }
    } finally {
      aiBatchBusyRef.current = false;
      dispatch({ type: "aiLoading", id: null });
    }
    return { done, failed };
  };

  /* ———— 自动分析：按设置的频率扫描最近 3 天未分析的新邮件 ———— */
  useEffect(() => {
    if (!state.booted || state.auth?.mode !== "user" || aiBatchBusyRef.current) return;
    const settings = loadAiSettings();
    if (!settings.autoAnalyze || !aiConfigured(settings)) return;

    // 频率节流：intervalMin=0 表示每次依赖变化都扫描（紧跟新邮件）；
    // 否则距上次扫描不足间隔就跳过
    const intervalMs = settings.autoAnalyzeIntervalMin * 60_000;
    if (intervalMs > 0 && Date.now() - lastAutoScanRef.current < intervalMs) return;

    const threeDaysAgo = Date.now() - 3 * 24 * 60 * 60 * 1000;
    const candidates = state.emails
      .filter(
        (e) =>
          e.folder === "inbox" &&
          !e.aiSummary &&
          !e.category &&
          !aiAttemptedRef.current.has(e.id) &&
          new Date(e.date).getTime() >= threeDaysAgo,
      )
      .slice(0, 20);
    if (!candidates.length) return;

    lastAutoScanRef.current = Date.now();
    void analyzeBatch(candidates, settings).then((r) => {
      if (!r) return;
      if (r.done) {
        actions.toast(
          `AI 已自动分析 ${r.done} 封最近邮件${r.failed ? `，${r.failed} 封失败` : ""}`,
          "default",
        );
      } else if (r.failed) {
        actions.toast(`AI 自动分析失败 ${r.failed} 封，请检查 AI 设置或稍后重试`, "warn");
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.emails, state.booted, state.auth?.mode]);

  /** 真实邮箱账户（+ 游客模式下的演示账户）合并后的完整账户列表 */
  const accounts = useMemo<Account[]>(() => {
    const real: Account[] = state.realMailboxes.map((m) => ({
      id: m.id,
      name: m.label || m.email,
      email: m.email,
      provider: (m.provider === "gmail" || m.provider === "outlook" || m.provider === "qq" || m.provider === "163"
        ? m.provider
        : "qq") as Account["provider"],
      color: avatarColor(m.email),
      real: true,
    }));
    // 登录会话只显示真实邮箱；演示账户仅游客模式可见
    if (state.auth?.mode === "user") return real;
    return [...real, ...ACCOUNTS];
  }, [state.realMailboxes, state.auth?.mode]);

  const value = useMemo<MailContextValue>(
    () => ({
      state,
      dispatch,
      accounts,
      visibleEmails,
      selectedEmail,
      unreadByAccount,
      unreadAll,
      actions,
    }),
    [state, accounts, visibleEmails, selectedEmail, unreadByAccount, unreadAll, actions],
  );

  return <MailContext.Provider value={value}>{children}</MailContext.Provider>;
}

export function useMail(): MailContextValue {
  const ctx = useContext(MailContext);
  if (!ctx) throw new Error("useMail 必须在 <MailProvider> 内使用");
  return ctx;
}
