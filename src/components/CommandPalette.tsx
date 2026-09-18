import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  Archive,
  Archive as ArchiveIcon,
  EyeOff,
  FileText,
  Inbox,
  List,
  Layers,
  Moon,
  Pencil,
  Reply,
  Rows2,
  Rows3,
  Search,
  Send,
  Star,
  Sun,
  Trash2,
  type LucideIcon,
} from "lucide-react";
import { useMail } from "../store/MailContext";
import {
  avatarColor,
  FOLDERS,
  initialOf,
  type FolderId,
} from "../types";
import { formatRelative } from "../services/mailService";

const FOLDER_ICONS: Record<FolderId, LucideIcon> = {
  inbox: Inbox,
  starred: Star,
  sent: Send,
  drafts: FileText,
  archive: Archive,
  trash: Trash2,
};

interface PalItem {
  id: string;
  group: "操作" | "邮件";
  icon?: LucideIcon;
  node?: ReactNode;
  title: string;
  sub?: string;
  hint?: string;
  run: () => void;
}

export function CommandPalette() {
  const { state, accounts, selectedEmail, actions } = useMail();
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const items = useMemo<PalItem[]>(() => {
    const acts: PalItem[] = [
      {
        id: "new",
        group: "操作",
        icon: Pencil,
        title: "写新邮件",
        hint: "C",
        run: () => actions.newCompose(),
      },
      ...FOLDERS.map((f) => ({
        id: `go-${f.id}`,
        group: "操作" as const,
        icon: FOLDER_ICONS[f.id],
        title: `前往 · ${f.label}`,
        hint: String(FOLDERS.indexOf(f) + 1),
        run: () => actions.setView({ folder: f.id, category: null }),
      })),
      {
        id: "acct-all",
        group: "操作",
        icon: Layers,
        title: "切换 · 全部收件箱",
        run: () => actions.setView({ accountId: "all" }),
      },
      ...accounts.map((a) => ({
        id: `acct-${a.id}`,
        group: "操作" as const,
        node: (
          <span className="avatar sm" style={{ background: a.color }}>
            {initialOf(a.name)}
          </span>
        ),
        title: `切换 · ${a.name}`,
        sub: a.email,
        run: () => actions.setView({ accountId: a.id }),
      })),
      {
        id: "theme",
        group: "操作",
        icon: state.theme === "dark" ? Sun : Moon,
        title: state.theme === "dark" ? "切换到亮色主题" : "切换到深色主题",
        hint: "T",
        run: () => actions.toggleTheme(),
      },
      {
        id: "density-cozy",
        group: "操作",
        icon: Rows3,
        title: "列表密度 · 舒适",
        run: () => actions.setDensity("cozy"),
      },
      {
        id: "density-compact",
        group: "操作",
        icon: Rows2,
        title: "列表密度 · 紧凑",
        run: () => actions.setDensity("compact"),
      },
      {
        id: "density-single",
        group: "操作",
        icon: List,
        title: "列表密度 · 单行",
        run: () => actions.setDensity("single"),
      },
    ];

    if (selectedEmail) {
      const e = selectedEmail;
      acts.push(
        {
          id: "reply",
          group: "操作",
          icon: Reply,
          title: `回复 · ${e.from.name}`,
          hint: "R",
          run: () => actions.replyTo(e),
        },
        {
          id: "archive-cur",
          group: "操作",
          icon: ArchiveIcon,
          title: "归档当前邮件",
          hint: "E",
          run: () => {
            actions.archive(e.id);
            actions.toast("已归档", "default");
          },
        },
        {
          id: "unread-cur",
          group: "操作",
          icon: EyeOff,
          title: "标记当前邮件为未读",
          run: () => actions.markRead(e.id, false),
        },
      );
    }

    if (state.emails.some((e) => e.folder === "trash")) {
      acts.push({
        id: "empty-trash",
        group: "操作",
        icon: Trash2,
        title: "清空垃圾箱",
        run: () => {
          actions.emptyTrash();
          actions.toast("垃圾箱已清空", "success");
        },
      });
    }

    const ql = q.trim().toLowerCase();
    if (!ql) return acts;

    const filteredActs = acts.filter((a) => a.title.toLowerCase().includes(ql));
    const mails: PalItem[] = state.emails
      .filter((e) =>
        `${e.subject}\n${e.from.name}\n${e.from.email}\n${e.body}`.toLowerCase().includes(ql),
      )
      .slice(0, 6)
      .map((e) => ({
        id: `mail-${e.id}`,
        group: "邮件" as const,
        node: (
          <span className="avatar sm" style={{ background: avatarColor(e.from.name || e.from.email) }}>
            {initialOf(e.from.name || e.from.email)}
          </span>
        ),
        title: e.subject,
        sub: `${e.from.name} · ${formatRelative(e.date)}`,
        run: () => {
          actions.setView({ accountId: e.accountId, folder: e.folder, category: null });
          actions.select(e.id);
        },
      }));

    return [...filteredActs, ...mails];
  }, [q, state.theme, state.emails, selectedEmail, accounts, actions]);

  // 查询变化后回到第一项；列表变短时夹住索引
  useEffect(() => {
    setActive(0);
  }, [q]);

  useEffect(() => {
    if (active >= items.length) setActive(Math.max(0, items.length - 1));
  }, [items.length, active]);

  useEffect(() => {
    listRef.current?.querySelector(".palette-item.active")?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const runItem = (item: PalItem) => {
    actions.closePalette();
    item.run();
  };

  const onKey = (ev: React.KeyboardEvent) => {
    if (ev.key === "ArrowDown") {
      ev.preventDefault();
      setActive((a) => Math.min(a + 1, items.length - 1));
    } else if (ev.key === "ArrowUp") {
      ev.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (ev.key === "Enter") {
      ev.preventDefault();
      const item = items[active];
      if (item) runItem(item);
    }
  };

  let lastGroup = "";

  return (
    <>
      <div className="overlay" onClick={actions.closePalette} />
      <div className="palette-wrap">
        <div className="palette" role="dialog" aria-label="命令面板">
          <div className="palette-input-row">
            <Search size={16} />
            <input
              ref={inputRef}
              value={q}
              onChange={(e) => setQ(e.target.value)}
              onKeyDown={onKey}
              placeholder="输入命令或搜索邮件…"
            />
            <kbd>Esc</kbd>
          </div>
          <div className="palette-results" ref={listRef}>
            {items.length === 0 && <div className="palette-group">没有匹配项</div>}
            {items.map((it, i) => {
              const header = it.group !== lastGroup ? it.group : null;
              lastGroup = it.group;
              const Icon = it.icon;
              return (
                <div key={it.id}>
                  {header && <div className="palette-group">{header}</div>}
                  <button
                    className={`palette-item ${i === active ? "active" : ""}`}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => runItem(it)}
                  >
                    {Icon ? <Icon size={16} /> : it.node}
                    <span className="p-title">{it.title}</span>
                    {it.sub && <span className="p-sub">{it.sub}</span>}
                    {it.hint && <kbd>{it.hint}</kbd>}
                  </button>
                </div>
              );
            })}
          </div>
          <div className="palette-foot">
            <span>
              <kbd>↑</kbd>
              <kbd>↓</kbd> 导航
            </span>
            <span>
              <kbd>↵</kbd> 执行
            </span>
            <span>
              <kbd>Esc</kbd> 关闭
            </span>
          </div>
        </div>
      </div>
    </>
  );
}
