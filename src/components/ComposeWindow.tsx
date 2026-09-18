import { useEffect, useRef, useState } from "react";
import { Paperclip, X } from "lucide-react";
import { useMail } from "../store/MailContext";
import type { ComposeMode } from "../types";

const MODE_LABEL: Record<ComposeMode, string> = {
  new: "新邮件",
  reply: "回复",
  replyAll: "回复全部",
  forward: "转发",
};

export function ComposeWindow() {
  const { state, accounts, actions } = useMail();
  const draft = state.compose;
  const [showCc, setShowCc] = useState(draft?.cc !== "");
  const toRef = useRef<HTMLInputElement>(null);
  const bodyRef = useRef<HTMLTextAreaElement>(null);

  // 自动聚焦：新邮件聚焦收件人，回复/转发聚焦正文
  useEffect(() => {
    if (!draft) return;
    if (draft.mode === "new" && !draft.to) toRef.current?.focus();
    else bodyRef.current?.focus();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft?.mode]);

  if (!draft) return null;

  const onKey = (ev: React.KeyboardEvent) => {
    if (ev.key === "Enter" && (ev.ctrlKey || ev.metaKey)) {
      ev.preventDefault();
      actions.sendCompose();
    }
  };

  return (
    <>
      <div className="overlay" onClick={() => actions.closeCompose(true)} />
      <div className="compose-wrap" onKeyDown={onKey}>
        <div className="compose" role="dialog" aria-label={MODE_LABEL[draft.mode]}>
          <div className="compose-head">
            <h3>{MODE_LABEL[draft.mode]}</h3>
            <select
              className="account-select"
              value={draft.accountId}
              onChange={(e) => actions.updateCompose({ accountId: e.target.value })}
              title="发件账户"
            >
              {accounts.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name} · {a.email}
                </option>
              ))}
            </select>
            <button className="icon-btn" title="保存并关闭 (Esc)" onClick={() => actions.closeCompose(true)}>
              <X size={16} />
            </button>
          </div>

          <div className="compose-fields">
            <div className="field-row">
              <label htmlFor="c-to">收件人</label>
              <input
                id="c-to"
                ref={toRef}
                value={draft.to}
                onChange={(e) => actions.updateCompose({ to: e.target.value })}
                placeholder="name@example.com，多人用逗号分隔"
              />
              {!showCc && (
                <button className="cc-toggle" onClick={() => setShowCc(true)}>
                  + 抄送
                </button>
              )}
            </div>
            {showCc && (
              <div className="field-row">
                <label htmlFor="c-cc">抄送</label>
                <input
                  id="c-cc"
                  value={draft.cc}
                  onChange={(e) => actions.updateCompose({ cc: e.target.value })}
                  placeholder="抄送人（可选）"
                />
              </div>
            )}
            <div className="field-row subject-row">
              <label htmlFor="c-subject">主题</label>
              <input
                id="c-subject"
                value={draft.subject}
                onChange={(e) => actions.updateCompose({ subject: e.target.value })}
                placeholder="写一个清晰的主题"
              />
            </div>
            <textarea
              ref={bodyRef}
              className="compose-body-input"
              value={draft.body}
              onChange={(e) => actions.updateCompose({ body: e.target.value })}
              placeholder="正文…"
            />
          </div>

          <div className="compose-foot">
            <button
              className="icon-btn"
              title="添加附件（演示版）"
              onClick={() => actions.toast("演示版暂不支持真实附件", "warn")}
            >
              <Paperclip size={16} />
            </button>
            <span style={{ flex: 1 }} />
            <button className="btn btn-ghost" onClick={() => actions.closeCompose(true)}>
              存草稿
            </button>
            <button className="btn btn-primary" onClick={() => actions.sendCompose()}>
              发送
              <kbd style={{ background: "rgba(255,255,255,.22)", borderColor: "transparent", color: "inherit" }}>
                Ctrl↵
              </kbd>
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
