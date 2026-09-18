import { useState } from "react";
import {
  Archive,
  FileText,
  Inbox,
  Layers,
  LogOut,
  Moon,
  Plus,
  RefreshCw,
  Sailboat,
  Send,
  Star,
  Sun,
  Trash2,
  type LucideIcon,
} from "lucide-react";
import { useMail } from "../store/MailContext";
import { realMailSupported } from "../services/realMailService";
import {
  CATEGORY_META,
  FOLDERS,
  initialOf,
  type FolderId,
  type SmartCategory,
} from "../types";

const FOLDER_ICONS: Record<FolderId, LucideIcon> = {
  inbox: Inbox,
  starred: Star,
  sent: Send,
  drafts: FileText,
  archive: Archive,
  trash: Trash2,
};

export function Sidebar() {
  const { state, accounts, unreadAll, unreadByAccount, actions } = useMail();
  const [spinning, setSpinning] = useState(false);

  const hasReal = state.realMailboxes.length > 0;
  const canAddReal = realMailSupported() && state.auth?.mode === "user";

  /** 侧边栏计数只跟随账户范围，不受搜索/分类筛选影响 */
  const folderCount = (f: FolderId): number | null => {
    let n = 0;
    for (const e of state.emails) {
      if (state.accountId !== "all" && e.accountId !== state.accountId) continue;
      if (f === "starred") {
        if (e.starred && e.folder !== "trash") n++;
      } else if (e.folder === f) {
        n++;
      }
    }
    return n > 0 ? n : null;
  };

  const folderUnread = (f: FolderId): number | null => {
    if (f !== "inbox") return null;
    let n = 0;
    for (const e of state.emails) {
      if (e.folder !== "inbox" || e.read) continue;
      if (state.accountId !== "all" && e.accountId !== state.accountId) continue;
      n++;
    }
    return n > 0 ? n : null;
  };

  const sync = () => {
    setSpinning(true);
    // 已接入真实邮箱时同步真邮件；否则仅模拟演示数据刷新
    if (hasReal) {
      void actions.syncRealMail().finally(() => setSpinning(false));
    } else {
      window.setTimeout(() => setSpinning(false), 900);
      actions.toast("当前是演示数据，未接入真实邮箱", "default");
    }
  };

  const toggleCategory = (c: SmartCategory) => {
    const next = state.category === c ? null : c;
    actions.setView({ category: next, folder: "inbox" });
  };

  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-mark">
          <Sailboat size={20} strokeWidth={1.8} />
        </span>
        <span>
          <span className="brand-name">Vela</span>
          <span className="brand-sub" style={{ display: "block" }}>
            轻帆邮件
          </span>
        </span>
      </div>

      <div className="side-section">
        <div className="side-label">账户</div>
        <button
          className={`side-row ${state.accountId === "all" ? "active" : ""}`}
          style={{ ["--i" as string]: 0 }}
          onClick={() => actions.setView({ accountId: "all" })}
        >
          <Layers size={16} className="row-icon" />
          <span className="row-label">
            <span className="row-name">全部收件箱</span>
            <span className="row-sub">{accounts.length} 个账户合一</span>
          </span>
          {unreadAll > 0 && <span className="badge">{unreadAll}</span>}
        </button>
        {accounts.map((a, i) => (
          <button
            key={a.id}
            className={`side-row ${state.accountId === a.id ? "active" : ""}`}
            style={{ ["--i" as string]: i + 1 }}
            onClick={() => actions.setView({ accountId: a.id })}
            title={a.real ? `${a.email} · 真实邮箱` : `${a.email} · 演示账户`}
          >
            <span className="avatar sm" style={{ background: a.color }}>
              {initialOf(a.name)}
            </span>
            <span className="row-label">
              <span className="row-name">
                {a.name}
                {a.real && <span className="real-dot" title="真实邮箱" />}
              </span>
              <span className="row-sub">{a.email}</span>
            </span>
            {unreadByAccount[a.id] > 0 && <span className="badge">{unreadByAccount[a.id]}</span>}
          </button>
        ))}

        {canAddReal && (
          <button
            className="side-row side-add"
            style={{ ["--i" as string]: accounts.length + 1 }}
            onClick={actions.openAddMailbox}
            title="接入 QQ / Gmail / 163 等真实邮箱"
          >
            <Plus size={16} className="row-icon" />
            <span className="row-label">接入真实邮箱</span>
          </button>
        )}
      </div>

      <div className="side-section">
        <div className="side-label">邮箱</div>
        {FOLDERS.map((f, i) => {
          const Icon = FOLDER_ICONS[f.id];
          const unread = folderUnread(f.id);
          const count = folderCount(f.id);
          return (
            <button
              key={f.id}
              className={`side-row ${state.folder === f.id ? "active" : ""}`}
              style={{ ["--i" as string]: i }}
              onClick={() => actions.setView({ folder: f.id, category: null })}
            >
              <Icon size={16} strokeWidth={1.8} className="row-icon" />
              <span className="row-label">{f.label}</span>
              {unread !== null ? (
                <span className="badge">{unread}</span>
              ) : count !== null ? (
                <span className="badge flat">{count}</span>
              ) : null}
            </button>
          );
        })}
      </div>

      <div className="side-section">
        <div className="side-label">智能分类</div>
        <div className="cat-chips">
          {(Object.keys(CATEGORY_META) as SmartCategory[]).map((c) => (
            <button
              key={c}
              className={`cat-chip ${state.category === c ? "active" : ""}`}
              style={{ ["--chip" as string]: CATEGORY_META[c].color }}
              onClick={() => toggleCategory(c)}
            >
              <span className="cat-dot" />
              {CATEGORY_META[c].label}
            </button>
          ))}
        </div>
      </div>

      <div className="side-foot">
        <div className="side-user" title={state.auth?.mode === "user" ? state.auth.username : "游客模式 · 数据存于本机"}>
          {state.auth?.mode === "user" ? (
            <span className="avatar sm" style={{ background: "var(--accent)" }}>
              {initialOf(state.auth.username)}
            </span>
          ) : null}
          <span className="side-user-name">
            {state.auth?.mode === "user" ? state.auth.username : "游客模式"}
          </span>
        </div>
        <span style={{ display: "flex", gap: 2 }}>
          <button
            className="icon-btn"
            title={hasReal ? "同步真实邮箱" : "同步（当前为演示数据）"}
            aria-label="同步"
            onClick={sync}
          >
            <RefreshCw size={15} className={spinning || state.syncing ? "spin" : ""} />
          </button>
          <button
            className="icon-btn"
            title={state.theme === "dark" ? "切换到亮色" : "切换到深色"}
            aria-label="切换主题"
            onClick={actions.toggleTheme}
          >
            {state.theme === "dark" ? <Sun size={15} /> : <Moon size={15} />}
          </button>
          <button
            className="icon-btn"
            title="退出登录"
            aria-label="退出登录"
            onClick={() => void actions.logout()}
          >
            <LogOut size={15} />
          </button>
        </span>
      </div>
    </aside>
  );
}
