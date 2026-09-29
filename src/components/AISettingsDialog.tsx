import { useMemo, useState } from "react";
import { AlertCircle, CheckCircle2, Loader2, RotateCcw, X } from "lucide-react";
import { useMail } from "../store/MailContext";
import { isDesktop } from "../services/realMailService";
import { errText } from "../services/errText";
import {
  AI_LANGUAGES,
  AI_PRESETS,
  AUTO_ANALYZE_INTERVALS,
  DEFAULT_PROMPT_TEMPLATE,
  loadAiSettings,
  saveAiSettings,
  testAiSettings,
  type AiSettings,
} from "../services/aiService";
import {
  loadNotifyFilter,
  notificationsEnabled,
  saveNotifyFilter,
  sendTestNotification,
  setNotificationsEnabled,
  type NotifyFilter,
} from "../services/notificationService";

/**
 * AI 设置面板：服务商 / API Key / 模型 / 输出语言 / 提示词模板。
 * 模板默认内置（含输出语言占位符 {{language}}），用户可完全自定义；
 * 换服务商时自动带出 API 地址与默认模型，也支持手改成任何 OpenAI 兼容服务。
 */
export function AISettingsDialog() {
  const { state, dispatch, actions } = useMail();

  const [settings, setSettings] = useState<AiSettings>(loadAiSettings);
  const [busy, setBusy] = useState<"test" | "notify" | null>(null);
  const [ok, setOk] = useState("");
  const [error, setError] = useState("");
  const [desktopNotify, setDesktopNotify] = useState(notificationsEnabled);
  const [notifyFilter, setNotifyFilter] = useState<NotifyFilter>(loadNotifyFilter);

  const preset = useMemo(
    () => AI_PRESETS.find((p) => p.id === settings.providerId),
    [settings.providerId],
  );

  const patch = (p: Partial<AiSettings>) => {
    setSettings((s) => ({ ...s, ...p }));
    setError("");
    setOk("");
  };

  const onProviderChange = (id: string) => {
    const p = AI_PRESETS.find((x) => x.id === id);
    if (!p) return;
    // 切换厂商：带出地址与模型，Key 换成该厂商记忆的那份（没填过则清空，
    // 绝不残留别家的 Key）；custom 保留现值供手填
    patch(
      id === "custom"
        ? { providerId: id, apiKey: settings.apiKeys[id] ?? "" }
        : {
            providerId: id,
            baseUrl: p.baseUrl,
            model: p.model,
            apiKey: settings.apiKeys[id] ?? "",
          },
    );
  };

  const doTest = async () => {
    setBusy("test");
    setError("");
    setOk("");
    try {
      const reply = await testAiSettings(settings);
      setOk(`连接成功，模型回复：「${reply.slice(0, 40)}」`);
    } catch (e) {
      setError(errText(e));
    } finally {
      setBusy(null);
    }
  };

  const doTestNotify = async () => {
    setBusy("notify");
    setError("");
    setOk("");
    try {
      const sent = await sendTestNotification();
      if (sent) setOk("测试通知已发送，请查看系统通知中心");
      else setError("系统通知权限被拒绝 —— 请在系统设置中允许 Vela 发送通知");
    } catch (e) {
      setError(errText(e));
    } finally {
      setBusy(null);
    }
  };

  const patchFilter = (p: Partial<NotifyFilter>) => {
    setNotifyFilter((f) => {
      const next = { ...f, ...p };
      saveNotifyFilter(next);
      return next;
    });
  };

  const doSave = () => {
    saveAiSettings(settings);
    actions.toast("AI 设置已保存", "success");
    actions.closeAiSettings();
  };

  return (
    <>
      <div className="overlay" onClick={actions.closeAiSettings} />
      <div className="compose-wrap">
        <div className="compose mailbox-dialog ai-dialog" role="dialog" aria-label="AI 设置">
          <div className="compose-head">
            <h3>设置</h3>
            <button
              className="icon-btn"
              title="关闭"
              aria-label="关闭"
              onClick={actions.closeAiSettings}
            >
              <X size={15} />
            </button>
          </div>

          <div className="mailbox-body">
            {!isDesktop() && (
              <div className="mailbox-note warn">
                <AlertCircle size={15} />
                <span>当前是浏览器环境，AI 分析需要在桌面应用中使用。</span>
              </div>
            )}

            <div className="settings-section-label">邮件同步</div>
            <label className="login-field">
              <span>自动检查新邮件频率</span>
              <select
                value={state.syncIntervalMin}
                onChange={(e) => dispatch({ type: "syncInterval", min: Number(e.target.value) })}
              >
                <option value={0}>仅手动同步</option>
                <option value={1}>每 1 分钟（默认）</option>
                <option value={5}>每 5 分钟</option>
                <option value={15}>每 15 分钟</option>
                <option value={60}>每 1 小时</option>
              </select>
            </label>

            {isDesktop() && (
              <>
                <label className="ai-toggle">
                  <input
                    type="checkbox"
                    checked={desktopNotify}
                    onChange={(e) => {
                      setDesktopNotify(e.target.checked);
                      setNotificationsEnabled(e.target.checked);
                      setError("");
                      setOk("");
                    }}
                  />
                  <span>来新邮件时发送系统通知（显示在系统通知中心）</span>
                </label>
                {desktopNotify && (
                  <>
                    <div className="login-field">
                      <span>
                        <button
                          className="chip"
                          style={{ ["--chip" as string]: "var(--muted)" }}
                          title="弹出一条样例通知，验证系统权限"
                          disabled={busy !== null}
                          onClick={() => void doTestNotify()}
                        >
                          {busy === "notify" && <Loader2 size={10} className="spin" />}
                          发送测试通知
                        </button>
                      </span>
                    </div>
                    <label className="ai-toggle">
                      <input
                        type="checkbox"
                        checked={notifyFilter.importantOnly}
                        onChange={(e) => patchFilter({ importantOnly: e.target.checked })}
                      />
                      <span>只提醒重要邮件（本地规则即时判断，不消耗 AI 额度）</span>
                    </label>
                    <label className="ai-toggle">
                      <input
                        type="checkbox"
                        checked={notifyFilter.quietEnabled}
                        onChange={(e) => patchFilter({ quietEnabled: e.target.checked })}
                      />
                      <span>免打扰时段（时段内不弹系统通知，应用内提示不受影响）</span>
                    </label>
                    {notifyFilter.quietEnabled && (
                      <div className="login-field">
                        <span>免打扰起止时间（可跨午夜）</span>
                        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                          <input
                            type="time"
                            value={notifyFilter.quietStart}
                            onChange={(e) => patchFilter({ quietStart: e.target.value })}
                          />
                          <span>至</span>
                          <input
                            type="time"
                            value={notifyFilter.quietEnd}
                            onChange={(e) => patchFilter({ quietEnd: e.target.value })}
                          />
                        </div>
                      </div>
                    )}
                    <div className="mailbox-note">
                      <AlertCircle size={15} />
                      <span>
                        桌面版通过 IMAP IDLE 实时感知新邮件（秒级提醒），关闭窗口后仍在
                        系统托盘中后台运行；真正退出请用托盘菜单「退出」。首次接入、
                        历史回填与已发送不会提醒。
                      </span>
                    </div>
                  </>
                )}
              </>
            )}

            <div className="settings-section-label">AI 分析</div>

            <label className="login-field">
              <span>服务商</span>
              <select value={settings.providerId} onChange={(e) => onProviderChange(e.target.value)}>
                {AI_PRESETS.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </select>
            </label>

            {preset?.hint && (
              <div className="mailbox-note">
                <AlertCircle size={15} />
                <span>{preset.hint}</span>
              </div>
            )}

            <label className="login-field">
              <span>API 地址（OpenAI 兼容根路径）</span>
              <input
                value={settings.baseUrl}
                onChange={(e) => patch({ baseUrl: e.target.value })}
                placeholder="https://open.bigmodel.cn/api/paas/v4"
                spellCheck={false}
              />
            </label>

            <label className="login-field">
              <span>API Key</span>
              <input
                type="password"
                value={settings.apiKey}
                onChange={(e) =>
                  patch({
                    apiKey: e.target.value,
                    apiKeys: { ...settings.apiKeys, [settings.providerId]: e.target.value },
                  })
                }
                placeholder={`粘贴 ${preset?.label ?? "当前厂商"} 的 API Key（仅保存在本机）`}
                autoComplete="off"
              />
            </label>

            <label className="login-field">
              <span>模型</span>
              <input
                value={settings.model}
                onChange={(e) => patch({ model: e.target.value })}
                placeholder="glm-4-flash"
                spellCheck={false}
              />
            </label>

            <label className="login-field">
              <span>输出语言（用于模板中的 {"{{language}}"}）</span>
              <select
                value={settings.language}
                onChange={(e) => patch({ language: e.target.value })}
              >
                {AI_LANGUAGES.map((l) => (
                  <option key={l} value={l}>
                    {l}
                  </option>
                ))}
              </select>
            </label>

            <div className="login-field">
              <span>自动分析频率</span>
              <select
                value={settings.autoAnalyzeIntervalMin}
                onChange={(e) => patch({ autoAnalyzeIntervalMin: Number(e.target.value) })}
                disabled={!settings.autoAnalyze}
              >
                {AUTO_ANALYZE_INTERVALS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </div>

            <label className="ai-toggle">
              <input
                type="checkbox"
                checked={settings.autoAnalyze}
                onChange={(e) => patch({ autoAnalyze: e.target.checked })}
              />
              <span>自动分析最近 3 天的新邮件（后台进行，消耗 API 额度；批量分析不受此限）</span>
            </label>

            <div className="login-field ai-template-field">
              <span>
                提示词模板
                <button
                  className="chip"
                  style={{ ["--chip" as string]: "var(--muted)" }}
                  title="恢复内置默认模板"
                  onClick={() => patch({ promptTemplate: DEFAULT_PROMPT_TEMPLATE })}
                >
                  <RotateCcw size={10} />
                  恢复默认
                </button>
              </span>
              <textarea
                className="ai-template"
                value={settings.promptTemplate}
                onChange={(e) => patch({ promptTemplate: e.target.value })}
                spellCheck={false}
              />
              <div className="mailbox-note">
                <AlertCircle size={15} />
                <span>
                  可用占位符：<code>{"{{language}}"}</code> <code>{"{{subject}}"}</code>{" "}
                  <code>{"{{from_name}}"}</code> <code>{"{{from_email}}"}</code>{" "}
                  <code>{"{{body}}"}</code>
                  。模板需让模型输出 JSON（category / summary / why），界面才能解析成分类与摘要。
                </span>
              </div>
            </div>

            {error && (
              <div className="login-error">
                <AlertCircle size={14} /> {error}
              </div>
            )}
            {ok && (
              <div className="mailbox-ok">
                <CheckCircle2 size={14} /> {ok}
              </div>
            )}
          </div>

          <div className="mailbox-foot">
            <button
              className="btn btn-ghost"
              disabled={busy !== null}
              onClick={() => void doTest()}
            >
              {busy === "test" && <Loader2 size={14} className="spin" />}
              测试连接
            </button>
            <button
              className="btn btn-primary"
              disabled={busy !== null || !settings.baseUrl.trim() || !settings.model.trim()}
              onClick={doSave}
            >
              保存
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
