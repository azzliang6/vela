import {
  Archive,
  ArrowLeft,
  FileArchive,
  FileCode2,
  FileImage,
  FileSpreadsheet,
  FileText,
  Forward,
  Loader2,
  Pencil,
  Reply,
  ReplyAll,
  Sailboat,
  Sparkles,
  Star,
  Trash2,
  Undo2,
  type LucideIcon,
} from "lucide-react";
import { useMail } from "../store/MailContext";
import { CATEGORY_META, avatarColor, initialOf, type Attachment } from "../types";
import { formatFull, formatSize } from "../services/mailService";
import { aiConfigured, loadAiSettings } from "../services/aiService";

const ATTACH_ICONS: Record<Attachment["kind"], LucideIcon> = {
  pdf: FileText,
  sheet: FileSpreadsheet,
  doc: FileText,
  image: FileImage,
  zip: FileArchive,
  code: FileCode2,
};

export function MailViewer() {
  const { state, selectedEmail: email, actions } = useMail();
  const aiReady = aiConfigured(loadAiSettings());
  const aiBusy = state.aiLoadingId === email?.id;

  if (!email) {
    return (
      <section className="reader-pane">
        <div className="reader-empty">
          <Sailboat className="sail" size={64} strokeWidth={1.2} />
          <h2>选一封信，开始阅读</h2>
          <p>
            <span>
              <kbd>J</kbd> / <kbd>K</kbd> 上下切换
            </span>
            <span>
              <kbd>Ctrl</kbd> + <kbd>K</kbd> 命令面板
            </span>
          </p>
        </div>
      </section>
    );
  }

  const catMeta = email.category ? CATEGORY_META[email.category] : null;
  const toNames = email.to.map((t) => t.name).join("、");
  const ccNames = email.cc?.length ? email.cc.map((c) => c.name).join("、") : "";

  return (
    <section className={`reader-pane ${email ? "has-mail" : "no-mail"}`}>
      <article className="reader-inner" key={email.id}>
        {email.folder === "trash" && (
          <div className="reader-chips">
            <button
              className="chip"
              style={{ ["--chip" as string]: "var(--danger)" }}
              onClick={() => actions.restore(email.id)}
            >
              <Undo2 size={12} />
              这封邮件在垃圾箱中 · 点击还原
            </button>
          </div>
        )}
        {email.folder === "drafts" && (
          <div className="reader-chips">
            <button
              className="chip"
              style={{ ["--chip" as string]: "var(--star)" }}
              onClick={() => actions.openDraft(email)}
            >
              <Pencil size={12} />
              这是一封草稿 · 继续编辑
            </button>
          </div>
        )}

        <div className="reader-toolbar">
          <span style={{ display: "flex", gap: 2 }}>
            <button
              className="icon-btn back-btn"
              title="返回列表"
              aria-label="返回列表"
              onClick={() => actions.select(null)}
            >
              <ArrowLeft size={17} />
            </button>
            <button className="icon-btn" title="回复 (R)" onClick={() => actions.replyTo(email)}>
              <Reply size={17} />
            </button>
            <button
              className="icon-btn"
              title="回复全部 (A)"
              onClick={() => actions.replyTo(email, true)}
            >
              <ReplyAll size={17} />
            </button>
            <button className="icon-btn" title="转发 (F)" onClick={() => actions.forward(email)}>
              <Forward size={17} />
            </button>
          </span>
          <span style={{ display: "flex", gap: 2 }}>
            <button
              className={`icon-btn star-btn ${email.starred ? "on" : ""}`}
              title={email.starred ? "取消星标 (S)" : "加星标 (S)"}
              onClick={() => actions.toggleStar(email.id)}
            >
              <Star size={16} fill={email.starred ? "currentColor" : "none"} />
            </button>
            {email.folder === "trash" ? (
              <button className="icon-btn accent" title="还原" onClick={() => actions.restore(email.id)}>
                <Undo2 size={16} />
              </button>
            ) : (
              <button
                className="icon-btn"
                title="归档 (E)"
                onClick={() => {
                  actions.archive(email.id);
                  actions.toast("已归档 · 可在归档文件夹找到", "default");
                }}
              >
                <Archive size={16} />
              </button>
            )}
            {email.folder !== "trash" && (
              <button
                className="icon-btn danger"
                title="移入垃圾箱"
                onClick={() => {
                  actions.trash(email.id);
                  actions.toast("已移入垃圾箱", "default");
                }}
              >
                <Trash2 size={16} />
              </button>
            )}
          </span>
        </div>

        <h1 className="reader-subject">{email.subject}</h1>

        <div className="reader-meta">
          <span
            className="avatar item-avatar"
            style={{ background: avatarColor(email.from.name || email.from.email) }}
          >
            {initialOf(email.from.name || email.from.email)}
          </span>
          <span className="reader-from">
            {email.from.name}
            <span>{email.from.email}</span>
          </span>
          <span className="reader-time">
            {formatFull(email.date)}
            {email.threadCount > 1 && <br />}
            {email.threadCount > 1 ? `会话共 ${email.threadCount} 封` : ""}
          </span>
        </div>
        <div className="reader-to-line">
          发至 {toNames}
          {ccNames ? `　·　抄送 ${ccNames}` : ""}
        </div>

        {!email.category && !email.aiSummary && email.folder !== "trash" && (
          <div className="reader-chips">
            <button
              className="chip analyze-chip"
              style={{ ["--chip" as string]: "var(--accent)" }}
              title={aiReady ? "调用大模型分类并生成摘要" : "用本地规则引擎分类并生成摘要（配置 AI 后走真实大模型）"}
              disabled={aiBusy || state.aiLoadingId !== null}
              onClick={() => actions.analyze(email.id)}
            >
              {aiBusy ? <Loader2 size={11} className="spin" /> : <Sparkles size={11} />}
              {aiBusy ? "AI 分析中…" : aiReady ? "AI 分析" : "本地规则分析"}
            </button>
          </div>
        )}

        {(catMeta || email.aiSummary) && (
          <div className="reader-chips">
            {catMeta && email.category && (
              <button
                className="chip"
                style={{ ["--chip" as string]: catMeta.color }}
                title="点击按此分类筛选"
                onClick={() => actions.setView({ category: email.category, folder: "inbox" })}
              >
                <Sparkles size={11} />
                AI 分类 · {catMeta.label}
              </button>
            )}
            {email.threadCount > 1 && (
              <span className="chip" style={{ ["--chip" as string]: "#6E675C" }}>
                会话 {email.threadCount} 封
              </span>
            )}
          </div>
        )}

        {email.aiSummary && (
          <div className="ai-card">
            <div className="ai-head">
              <Sparkles size={13} />
              Vela AI 摘要
              {aiReady && email.folder !== "trash" && (
                <button
                  className="chip ai-reanalyze"
                  title="用大模型重新分析这封邮件"
                  disabled={aiBusy || state.aiLoadingId !== null}
                  onClick={() => actions.analyze(email.id)}
                >
                  {aiBusy ? <Loader2 size={10} className="spin" /> : <Sparkles size={10} />}
                  重新分析
                </button>
              )}
            </div>
            <div className="ai-body">{email.aiSummary}</div>
            {email.aiWhy && (
              <div className="ai-why">
                <strong>为什么重要</strong>
                <span>{email.aiWhy}</span>
              </div>
            )}
          </div>
        )}

        {email.attachments.length > 0 && (
          <div className="attach-row">
            {email.attachments.map((a) => {
              const Icon = ATTACH_ICONS[a.kind];
              return (
                <button
                  key={a.id}
                  className="attach-chip"
                  onClick={() => actions.toast(`演示数据：${a.name} 不可下载`, "warn")}
                  title={`${a.name} · ${formatSize(a.sizeKb)}`}
                >
                  <Icon size={14} />
                  {a.name}
                  <span className="size">{formatSize(a.sizeKb)}</span>
                </button>
              );
            })}
          </div>
        )}

        {email.htmlBody ? (
          <div className="mail-html-wrap">
            {/*
              沙箱 iframe 渲染邮件 HTML 正文：sandbox 置空串禁掉脚本/表单/跳转，
              只保留图片与样式；srcdoc 与外部隔离，无法触碰应用本身。
              高度固定内部滚动（沙箱域不可读，没法自适应内容高度）。
            */}
            <iframe
              className="mail-html"
              title="邮件正文"
              sandbox=""
              srcDoc={email.htmlBody}
            />
            {email.body.trim() && <div className="mail-html-tip">以上为 HTML 版正文 · 部分内嵌图片可能不显示</div>}
          </div>
        ) : (
          <div className="mail-body">
            {email.body.split(/\n\n+/).map((p, i) => (
              <p key={i}>{p}</p>
            ))}
          </div>
        )}
      </article>
    </section>
  );
}
