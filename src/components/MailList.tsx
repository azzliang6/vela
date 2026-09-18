import { useEffect, useRef, useState } from "react";
import {
  Archive,
  Check,
  CheckCheck,
  Eye,
  EyeOff,
  List,
  Paperclip,
  Rows2,
  Rows3,
  Search,
  Star,
  Trash2,
  Undo2,
  X,
} from "lucide-react";
import { useMail } from "../store/MailContext";
import {
  CATEGORY_META,
  FOLDERS,
  avatarColor,
  initialOf,
  type Density,
  type Email,
} from "../types";
import { formatRelative, previewOf } from "../services/mailService";

const DENSITY_OPTIONS: { id: Density; label: string; icon: typeof Rows3 }[] = [
  { id: "cozy", label: "舒适", icon: Rows3 },
  { id: "compact", label: "紧凑", icon: Rows2 },
  { id: "single", label: "单行", icon: List },
];

export function MailList() {
  const { state, accounts, visibleEmails, actions } = useMail();

  const unread = visibleEmails.filter((e) => !e.read).length;
  const folderLabel = FOLDERS.find((f) => f.id === state.folder)?.label ?? "";
  const scopeLabel =
    state.accountId === "all"
      ? "全部账户"
      : (accounts.find((a) => a.id === state.accountId)?.name ?? "");
  const catLabel = state.category ? ` · ${CATEGORY_META[state.category].label}` : "";
  const checkedCount = state.checkedIds.length;
  const anyChecked = checkedCount > 0;

  return (
    <section className="mail-list-pane" data-density={state.density}>
      <header className="list-head">
        <div className="list-title-row">
          <h1 className="list-title">{state.search ? "搜索结果" : folderLabel}</h1>
          {unread > 0 && <span className="list-count">{unread} 未读</span>}
        </div>
        <div className="list-sub">
          {visibleEmails.length} 封邮件 · {scopeLabel}
          {catLabel}
        </div>
        <div className="list-tools">
          <div className="search-box">
            <Search size={14} />
            <input
              id="vela-search"
              placeholder="搜索邮件，按 / 聚焦"
              value={state.search}
              onChange={(e) => actions.setSearch(e.target.value)}
            />
            {state.search ? (
              <button
                className="icon-btn"
                style={{ width: 22, height: 22 }}
                title="清除搜索"
                onClick={() => actions.setSearch("")}
              >
                <X size={13} />
              </button>
            ) : (
              <kbd>/</kbd>
            )}
          </div>
          <div className="seg" role="group" aria-label="列表密度">
            {DENSITY_OPTIONS.map((d) => (
              <button
                key={d.id}
                className={state.density === d.id ? "on" : ""}
                title={`列表密度 · ${d.label}`}
                aria-label={`列表密度 · ${d.label}`}
                onClick={() => actions.setDensity(d.id)}
              >
                <d.icon size={14} />
              </button>
            ))}
          </div>
        </div>
      </header>

      <div className="list-scroll">
        {visibleEmails.length === 0 ? (
          <div className="list-empty">
            <div className="big">没有匹配的邮件</div>
            <div>换个关键词，或清除当前筛选条件</div>
            <button
              onClick={() => {
                actions.setSearch("");
                actions.setView({ category: null });
              }}
            >
              清除搜索与分类筛选
            </button>
          </div>
        ) : (
          <>
            {visibleEmails.map((e, i) => (
              <MailListItem
                key={e.id}
                email={e}
                index={i}
                selected={e.id === state.selectedId}
                checked={state.checkedIds.includes(e.id)}
                accountColor={
                  state.accountId === "all"
                    ? (accounts.find((a) => a.id === e.accountId)?.color ?? "#999")
                    : null
                }
              />
            ))}
            {anyChecked && (
              <BatchBar count={checkedCount} onClear={() => actions.checksClear()} />
            )}
          </>
        )}
      </div>
    </section>
  );
}

function BatchBar({ count, onClear }: { count: number; onClear: () => void }) {
  const { state, actions } = useMail();
  const ids = state.checkedIds;
  const allRead = ids.length > 0;

  return (
    <div className="batch-bar" role="toolbar" aria-label="批量操作">
      <span className="bb-count">已选 {count}</span>
      <span className="bb-sep" />
      {allRead && (
        <>
          <button className="bb-btn" onClick={() => actions.batchRead(ids, true)}>
            <CheckCheck size={13} />
            已读
          </button>
          <button className="bb-btn" onClick={() => actions.batchRead(ids, false)}>
            <EyeOff size={13} />
            未读
          </button>
          <button className="bb-btn" onClick={() => actions.batchStar(ids, true)}>
            <Star size={13} />
            星标
          </button>
          <button className="bb-btn" onClick={() => actions.batchStar(ids, false)}>
            <Star size={13} />
            去星
          </button>
        </>
      )}
      <button className="bb-btn" onClick={() => actions.batchArchive(ids)}>
        <Archive size={13} />
        归档
      </button>
      <button className="bb-btn" onClick={() => actions.batchTrash(ids)}>
        <Trash2 size={13} />
        垃圾箱
      </button>
      <span className="bb-sep" />
      <button className="bb-btn" onClick={onClear} title="清除选择 (Esc)">
        <X size={13} />
      </button>
    </div>
  );
}

function MailListItem({
  email,
  index,
  selected,
  checked,
  accountColor,
}: {
  email: Email;
  index: number;
  selected: boolean;
  checked: boolean;
  accountColor: string | null;
}) {
  const { actions } = useMail();
  const [pop, setPop] = useState(false);
  const ref = useRef<HTMLDivElement | null>(null);
  const lastCheckedRef = useRef<string | null>(null);

  useEffect(() => {
    if (selected) ref.current?.scrollIntoView({ block: "nearest" });
  }, [selected]);

  const catColor = email.category ? CATEGORY_META[email.category].color : null;
  const inTrash = email.folder === "trash";
  const inArchive = email.folder === "archive";

  const star = (ev: React.MouseEvent) => {
    ev.stopPropagation();
    setPop(true);
    window.setTimeout(() => setPop(false), 380);
    actions.toggleStar(email.id);
  };

  const onCheckClick = (ev: React.MouseEvent) => {
    ev.stopPropagation();
    if (ev.shiftKey && lastCheckedRef.current) {
      // 范围勾选由父层结合可见列表处理：这里只记录锚点后委托
      actions.checksSet([email.id], "union");
    }
    actions.toggleCheck(email.id);
    lastCheckedRef.current = email.id;
  };

  const onItemClick = (ev: React.MouseEvent) => {
    if (ev.ctrlKey || ev.metaKey) {
      ev.preventDefault();
      actions.toggleCheck(email.id);
      return;
    }
    actions.select(email.id);
  };

  return (
    <div
      ref={ref}
      role="button"
      tabIndex={0}
      className={`mail-item ${email.read ? "" : "unread"} ${selected ? "selected" : ""} ${checked ? "checked" : ""}`}
      style={{ ["--i" as string]: Math.min(index, 14) }}
      onClick={onItemClick}
      onKeyDown={(ev) => {
        if (ev.key === "Enter" || ev.key === " ") {
          ev.preventDefault();
          actions.select(email.id);
        }
      }}
    >
      <span className="lead-cell">
        <span className="unread-dot" />
        <button
          className={`check ${checked ? "on" : ""}`}
          title={checked ? "取消勾选" : "勾选（Shift 范围选）"}
          aria-checked={checked}
          role="checkbox"
          onClick={onCheckClick}
        >
          {checked && <Check size={11} strokeWidth={3} />}
        </button>
      </span>

      <span
        className="avatar item-avatar"
        style={{
          background: avatarColor(email.from.name || email.from.email),
          ...(accountColor ? { boxShadow: `0 0 0 2px ${accountColor}` } : {}),
        }}
        title={accountColor ? "账户标识" : undefined}
      >
        {initialOf(email.from.name || email.from.email)}
      </span>

      <span className="item-main">
        <span className="item-top">
          <span className="item-from">{email.from.name}</span>
          <span className="item-subject">{email.subject}</span>
          {catColor && (
            <span
              className="item-cat"
              style={{ background: catColor }}
              title={email.category ? CATEGORY_META[email.category].label : ""}
            />
          )}
          {email.threadCount > 1 && (
            <span className="thread-chip" title={`会话共 ${email.threadCount} 封`}>
              {email.threadCount}
            </span>
          )}
        </span>
        <span className="item-preview">
          {email.attachments.length > 0 && (
            <Paperclip size={11} style={{ flex: "none", marginTop: 2 }} />
          )}
          <span className="preview-text">{previewOf(email)}</span>
        </span>
      </span>

      <span className="item-meta">
        <span className="item-time">{formatRelative(email.date)}</span>
        <span className="item-actions" onClick={(ev) => ev.stopPropagation()}>
          <button
            className={`icon-btn star-btn ${email.starred ? "on" : ""} ${pop ? "pop" : ""}`}
            title={email.starred ? "取消星标" : "加星标"}
            onClick={star}
          >
            <Star size={14} fill={email.starred ? "currentColor" : "none"} />
          </button>
          {inTrash || inArchive ? (
            <button
              className="icon-btn accent"
              title="还原到收件箱"
              onClick={() => actions.restore(email.id)}
            >
              <Undo2 size={14} />
            </button>
          ) : (
            <button
              className="icon-btn"
              title="归档"
              onClick={() => actions.archive(email.id)}
            >
              <Archive size={14} />
            </button>
          )}
          {!inTrash && (
            <button
              className="icon-btn danger"
              title="移入垃圾箱"
              onClick={() => actions.trash(email.id)}
            >
              <Trash2 size={14} />
            </button>
          )}
          <button
            className="icon-btn"
            title={email.read ? "标为未读" : "标为已读"}
            onClick={() => actions.markRead(email.id, !email.read)}
          >
            {email.read ? <EyeOff size={14} /> : <Eye size={14} />}
          </button>
        </span>
      </span>
    </div>
  );
}
