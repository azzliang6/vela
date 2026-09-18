import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useReducer,
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
import { api, type ServerEmail } from "../services/api";
import { realMail, realMailSupported } from "../services/realMailService";

export type Theme = "light" | "dark";

/** null = 未登录（显示登录页）；guest = 游客模式（localStorage）；user = PG 账户 */
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
  /** 移除某个账户及其全部邮件 */
  | { type: "removeAccountEmails"; accountId: string }
  /** 合并从服务器拉回的真实邮件（按 id 去重，新邮件插在前面） */
  | { type: "mergeReal"; emails: Email[] };

const LS_THEME = "vela:theme";
const LS_PATCHES = "vela:patches-v1";
const LS_USER = "vela:user-emails-v1";
const LS_DENSITY = "vela:density";
const LS_TOKEN = "vela:token";
const LS_GUEST = "vela:guest";

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
  };
}

function toggleIn(arr: string[], id: string): string[] {
  return arr.includes(id) ? arr.filter((x) => x !== id) : [...arr, id];
}

/** 服务端结构 -> 前端 Email（folder/category 枚举收紧，null 归一为 undefined） */
function fromServer(e: ServerEmail): Email {
  return {
    ...e,
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

function freshMockCopy(): Email[] {
  return MOCK_EMAILS.map((e) => ({ ...e }));
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
    login: (username: string, password: string) => Promise<void>;
    register: (username: string, password: string) => Promise<void>;
    logout: () => Promise<void>;
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
    toast: (text: string, tone?: Toast["tone"]) => void;
    dismissToast: (id: number) => void;
    /* ———— 真实邮箱 ———— */
    /** 载入已接入的真实邮箱账户列表 */
    loadRealMailboxes: () => Promise<void>;
    /** 同步全部真实邮箱（拉取新邮件） */
    syncRealMail: (silent?: boolean) => Promise<void>;
    /** 打开 / 关闭「接入真实邮箱」向导 */
    openAddMailbox: () => void;
    closeAddMailbox: () => void;
    /** 移除一个真实邮箱账户 */
    removeRealMailbox: (accountId: string) => Promise<void>;
  };
}

const MailContext = createContext<MailContextValue | null>(null);

export function MailProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, undefined, initialState);

  /** 以服务端邮箱进入用户会话；服务端为空（新账户）时用演示数据播种，交由同步 effect 上传 */
  const enterUserSession = async (token: string, username: string) => {
    const { emails } = await api.getEmails(token);
    const list = emails.length ? emails.map(fromServer) : freshMockCopy();
    dispatch({ type: "setAuth", auth: { mode: "user", username, token }, emails: list });
  };

  /** 载入真实邮箱账户列表（仅桌面版可用，失败不阻断其它功能） */
  const loadRealMailboxes = async () => {
    if (!realMailSupported() || state.auth?.mode !== "user") return;
    try {
      const list = await realMail.listAccounts(state.auth.token);
      dispatch({ type: "realMailboxes", mailboxes: list });
    } catch {
      /* 数据库未就绪等情况：静默失败，不影响演示功能 */
    }
  };

  // 启动：校验本地会话令牌；无令牌但有游客标记则恢复游客会话
  useEffect(() => {
    (async () => {
      const token = localStorage.getItem(LS_TOKEN);
      if (token) {
        try {
          const me = await api.getMe(token);
          await enterUserSession(token, me.username);
          return;
        } catch {
          localStorage.removeItem(LS_TOKEN);
        }
      }
      if (localStorage.getItem(LS_GUEST) === "1") {
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
  }, [state.booted, state.auth?.mode, state.auth?.mode === "user" ? state.auth.token : ""]);

  // 持久化：主题与密度始终本地
  useEffect(() => {
    try {
      localStorage.setItem(LS_THEME, state.theme);
      localStorage.setItem(LS_DENSITY, state.density);
    } catch {
      /* ignore */
    }
  }, [state.theme, state.density]);

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

  // 持久化：用户模式防抖同步到 PostgreSQL
  useEffect(() => {
    if (!state.booted || state.auth?.mode !== "user") return;
    const token = state.auth.token;
    const t = window.setTimeout(() => {
      api.syncEmails(token, state.emails).catch((err: Error) => {
        dispatch({
          type: "toastPush",
          toast: { id: Date.now(), text: `同步失败：${err.message}`, tone: "warn" },
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
      login: async (username, password) => {
        const r = await api.login(username, password);
        localStorage.setItem(LS_TOKEN, r.token);
        localStorage.removeItem(LS_GUEST);
        await enterUserSession(r.token, r.username);
      },
      register: async (username, password) => {
        const r = await api.register(username, password);
        localStorage.setItem(LS_TOKEN, r.token);
        localStorage.removeItem(LS_GUEST);
        await enterUserSession(r.token, r.username);
      },
      logout: async () => {
        const token = state.auth?.mode === "user" ? state.auth.token : null;
        localStorage.removeItem(LS_TOKEN);
        localStorage.removeItem(LS_GUEST);
        if (token) {
          try {
            await api.logout(token);
          } catch {
            /* 服务端会话即使未清理也会随用户改密/登出失效 */
          }
        }
        dispatch({ type: "setAuth", auth: null, emails: [] });
      },
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
            .setFlags(target, { read: true }, state.auth.token)
            .catch((err: Error) => notify(`已读状态未能同步到服务器：${err.message}`, "warn"));
        }
      },
      toggleStar: (id) => {
        const target = state.emails.find((e) => e.id === id);
        dispatch({ type: "star", id });
        if (target?.real && state.auth?.mode === "user") {
          void realMail
            .setFlags(target, { starred: !target.starred }, state.auth.token)
            .catch((err: Error) => notify(`星标未能同步到服务器：${err.message}`, "warn"));
        }
      },
      markRead: (id, read) => {
        const target = state.emails.find((e) => e.id === id);
        dispatch({ type: "read", id, read });
        if (target?.real && state.auth?.mode === "user") {
          void realMail
            .setFlags(target, { read }, state.auth.token)
            .catch((err: Error) => notify(`已读状态未能同步到服务器：${err.message}`, "warn"));
        }
      },
      archive: (id) => {
        const target = state.emails.find((e) => e.id === id);
        moveTo([id], "archive", "已归档");
        if (target?.real && state.auth?.mode === "user") {
          void realMail
            .archive(target, state.auth.token)
            .catch((err: Error) => notify(`服务器归档失败：${err.message}`, "warn"));
        }
      },
      trash: (id) => {
        const target = state.emails.find((e) => e.id === id);
        moveTo([id], "trash", "已移入垃圾箱");
        if (target?.real && state.auth?.mode === "user") {
          void realMail
            .trash(target, state.auth.token)
            .catch((err: Error) => notify(`服务器删除失败：${err.message}`, "warn"));
        }
      },
      restore: (id) =>
        dispatch({ type: "move", id, folder: "inbox", nextSelectedId: null }),
      emptyTrash: () => {
        const removed = state.emails.filter((e) => e.folder === "trash");
        if (!removed.length) return;
        dispatch({ type: "emptyTrash" });
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
          const token = state.auth.token;
          for (const id of ids) {
            const e = state.emails.find((x) => x.id === id);
            if (e?.real) {
              void realMail.setFlags(e, { read }, token).catch(() => {
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
          const token = state.auth.token;
          for (const id of ids) {
            const e = state.emails.find((x) => x.id === id);
            if (e?.real) {
              void realMail.setFlags(e, { starred }, token).catch(() => {
                /* 单封失败不打断整批 */
              });
            }
          }
        }
      },
      batchArchive: (ids) => {
        moveTo(ids, "archive", "已归档");
        if (state.auth?.mode === "user") {
          const token = state.auth.token;
          for (const id of ids) {
            const e = state.emails.find((x) => x.id === id);
            if (e?.real) void realMail.archive(e, token).catch(() => {});
          }
        }
      },
      batchTrash: (ids) => {
        moveTo(ids, "trash", "已移入垃圾箱");
        if (state.auth?.mode === "user") {
          const token = state.auth.token;
          for (const id of ids) {
            const e = state.emails.find((x) => x.id === id);
            if (e?.real) void realMail.trash(e, token).catch(() => {});
          }
        }
      },
      openPalette: () => dispatch({ type: "palette", open: true }),
      closePalette: () => dispatch({ type: "palette", open: false }),
      newCompose: (accountId) => {
        const acct =
          accountId ?? (state.accountId === "all" ? ACCOUNTS[0].id : state.accountId);
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
          const account = ACCOUNTS.find((a) => a.id === draft.accountId) ?? ACCOUNTS[0];
          const email: Email = {
            id: state.composeDraftId ?? `draft-${Date.now()}`,
            accountId: draft.accountId,
            folder: "drafts",
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
          const token = state.auth.token;
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
              token,
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
            .catch((err: Error) => {
              // 发送失败时保留写信窗口与草稿，用户可重试或修改后重发
              dispatch({
                type: "toastPush",
                toast: { id: Date.now(), text: `发送失败：${err.message}`, tone: "warn", ttl: 6000 },
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
        if (!target || target.category) return;
        dispatch({ type: "analyze", id });
        if (classifyRules(target)) {
          notify("本地规则分析完成", "success");
        } else {
          notify("规则未命中，保持未分类", "warn");
        }
      },
      toast: (text, tone = "default") =>
        dispatch({ type: "toastPush", toast: { id: Date.now(), text, tone } }),
      dismissToast: (id) => dispatch({ type: "toastPop", id }),
      /* ———— 真实邮箱 ———— */
      loadRealMailboxes,
      syncRealMail: async (silent = false) => {
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

        dispatch({ type: "syncing", on: true });
        try {
          const { emails: incoming, failures, hasMoreHistory } = await realMail.syncAll(
            state.auth.token,
          );
          dispatch({ type: "mergeReal", emails: incoming });

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
          dispatch({
            type: "toastPush",
            toast: {
              id: Date.now(),
              text: `同步失败：${e instanceof Error ? e.message : "未知错误"}`,
              tone: "warn",
              ttl: 6000,
            },
          });
        } finally {
          dispatch({ type: "syncing", on: false });
        }
      },
      openAddMailbox: () => dispatch({ type: "addMailbox", open: true }),
      closeAddMailbox: () => dispatch({ type: "addMailbox", open: false }),
      removeRealMailbox: async (accountId) => {
        if (state.auth?.mode !== "user") return;
        try {
          await realMail.removeAccount(accountId, state.auth.token);
          await loadRealMailboxes();
          // 一并清掉本地该账户的邮件，避免留下无法操作的孤儿数据
          dispatch({ type: "removeAccountEmails", accountId });
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

  /** 演示账户 + 真实邮箱账户合并后的完整账户列表 */
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
    return [...real, ...ACCOUNTS];
  }, [state.realMailboxes]);

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
