import { useEffect, useMemo, useState } from "react";
import { AlertCircle, CheckCircle2, Loader2, X } from "lucide-react";
import { useMail } from "../store/MailContext";
import { realMail, realMailSupported } from "../services/realMailService";
import { errText } from "../services/errText";
import type { ProviderPreset } from "../types";

/**
 * 接入真实邮箱的向导。
 *
 * 流程：选服务商 → 填邮箱与授权码 → 测试连通 → 保存。
 * 保存前一定会先测试连通性，避免把错误的授权码写进数据库。
 */
export function AddMailboxDialog() {
  const { actions } = useMail();

  const [presets, setPresets] = useState<ProviderPreset[]>([]);
  const [provider, setProvider] = useState("qq");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [label, setLabel] = useState("");
  const [imapHost, setImapHost] = useState("");
  const [smtpHost, setSmtpHost] = useState("");
  const [busy, setBusy] = useState<"test" | "save" | null>(null);
  const [ok, setOk] = useState("");
  const [error, setError] = useState("");

  const current = useMemo(
    () => presets.find((p) => p.id === provider),
    [presets, provider],
  );
  const isCustom = provider === "custom";

  // 载入预设；同时按邮箱域名自动选中服务商
  useEffect(() => {
    if (!realMailSupported()) return;
    realMail
      .providers()
      .then(setPresets)
      .catch((e: Error) => setError(e.message));
  }, []);

  // 输入邮箱时自动匹配服务商（用户手动改过就不再覆盖）。
  // 域名还没输到「xxx.」之前不猜（否则输入 @ 的瞬间会被切到 custom，
  // 看起来就像服务商被清空）；也从不自动跳到 custom。
  const onEmailChange = async (v: string) => {
    setEmail(v);
    setError("");
    setOk("");
    if (!v.includes("@") || isCustom) return;
    const domain = v.slice(v.lastIndexOf("@") + 1);
    if (!domain.includes(".")) return;
    try {
      const guess = await realMail.guessProvider(v.trim());
      if (guess && guess !== "custom" && guess !== provider) setProvider(guess);
    } catch {
      /* 匹配失败不打扰用户 */
    }
  };

  const input = () => ({
    provider,
    email: email.trim(),
    password,
    label: label.trim() || email.trim(),
    imapHost: imapHost.trim(),
    smtpHost: smtpHost.trim(),
  });

  const doTest = async () => {
    setBusy("test");
    setError("");
    setOk("");
    try {
      const folders = await realMail.testConnection(input());
      setOk(`连接成功，共发现 ${folders.length} 个文件夹`);
    } catch (e) {
      setError(errText(e));
    } finally {
      setBusy(null);
    }
  };

  const doSave = async () => {
    setBusy("save");
    setError("");
    setOk("");
    try {
      const acc = await realMail.addAccount(input());
      actions.toast(`已接入 ${acc.email}`, "success");
      await actions.loadRealMailboxes();
      actions.closeAddMailbox();
    } catch (e) {
      setError(errText(e));
    } finally {
      setBusy(null);
    }
  };

  const disabled = busy !== null || !email.trim() || !password;

  return (
    <>
      <div className="overlay" onClick={() => actions.closeAddMailbox()} />
      <div className="compose-wrap">
        <div className="compose mailbox-dialog" role="dialog" aria-label="接入真实邮箱">
          <div className="compose-head">
            <h3>接入真实邮箱</h3>
            <button
              className="icon-btn"
              title="关闭"
              aria-label="关闭"
              onClick={() => actions.closeAddMailbox()}
            >
              <X size={15} />
            </button>
          </div>

          <div className="mailbox-body">
            {!realMailSupported() && (
              <div className="mailbox-note warn">
                <AlertCircle size={15} />
                <span>
                  当前是浏览器环境，无法使用真实邮箱功能（需要原生 IMAP/SMTP）。
                  请用 <code>pnpm tauri dev</code> 启动桌面版。
                </span>
              </div>
            )}

            <label className="login-field">
              <span>服务商</span>
              <select value={provider} onChange={(e) => setProvider(e.target.value)}>
                {presets.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </select>
            </label>

            {current?.hint && (
              <div className="mailbox-note">
                <AlertCircle size={15} />
                <span>{current.hint}</span>
              </div>
            )}

            <label className="login-field">
              <span>邮箱地址</span>
              <input
                value={email}
                onChange={(e) => void onEmailChange(e.target.value)}
                placeholder="you@example.com"
                autoComplete="username"
              />
            </label>

            <label className="login-field">
              <span>授权码 / 密码</span>
              <input
                type="password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  setError("");
                }}
                placeholder="邮箱授权码，不是网页登录密码"
                autoComplete="current-password"
              />
            </label>

            <label className="login-field">
              <span>显示名称（可选）</span>
              <input
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                placeholder="留空则用邮箱地址"
              />
            </label>

            {isCustom && (
              <>
                <label className="login-field">
                  <span>IMAP 服务器</span>
                  <input
                    value={imapHost}
                    onChange={(e) => setImapHost(e.target.value)}
                    placeholder="imap.example.com"
                  />
                </label>
                <label className="login-field">
                  <span>SMTP 服务器</span>
                  <input
                    value={smtpHost}
                    onChange={(e) => setSmtpHost(e.target.value)}
                    placeholder="smtp.example.com"
                  />
                </label>
              </>
            )}

            {current && !isCustom && (
              <div className="mailbox-servers">
                <span>IMAP {current.imapHost}:{current.imapPort}</span>
                <span>SMTP {current.smtpHost}:{current.smtpPort}</span>
              </div>
            )}

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
            <button className="btn btn-ghost" disabled={disabled} onClick={() => void doTest()}>
              {busy === "test" && <Loader2 size={14} className="spin" />}
              测试连接
            </button>
            <button className="btn btn-primary" disabled={disabled} onClick={() => void doSave()}>
              {busy === "save" && <Loader2 size={14} className="spin" />}
              保存并接入
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
