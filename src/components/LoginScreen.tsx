import { useState } from "react";
import { LogIn, Sailboat, UserPlus } from "lucide-react";
import { useMail } from "../store/MailContext";

export function LoginScreen() {
  const { actions } = useMail();
  const [tab, setTab] = useState<"login" | "register">("login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      if (tab === "login") await actions.login(username.trim(), password);
      else await actions.register(username.trim(), password);
      // 成功后 setAuth 会切换视图，组件卸载
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作失败");
      setBusy(false);
    }
  };

  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={submit}>
        <div className="login-brand">
          <span className="brand-mark">
            <Sailboat size={26} strokeWidth={1.8} />
          </span>
          <div>
            <div className="brand-name">Vela 轻帆</div>
            <div className="login-sub">现代、美观、快速的跨平台邮件客户端</div>
          </div>
        </div>

        <div className="login-tabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={tab === "login"}
            className={tab === "login" ? "on" : ""}
            onClick={() => {
              setTab("login");
              setError("");
            }}
          >
            登录
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "register"}
            className={tab === "register" ? "on" : ""}
            onClick={() => {
              setTab("register");
              setError("");
            }}
          >
            注册
          </button>
        </div>

        <label className="login-field">
          <span>用户名</span>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="2-30 位字母 / 数字 / 中文"
            autoComplete="username"
            required
          />
        </label>
        <label className="login-field">
          <span>密码</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={tab === "register" ? "至少 6 位" : "你的密码"}
            autoComplete={tab === "register" ? "new-password" : "current-password"}
            required
          />
        </label>

        {error && <div className="login-error">{error}</div>}

        <button className="btn btn-primary login-submit" disabled={busy}>
          {tab === "login" ? <LogIn size={15} /> : <UserPlus size={15} />}
          {busy ? "请稍候…" : tab === "login" ? "登录" : "创建账户并进入"}
        </button>

        <div className="login-alt">
          <span className="login-alt-line" />
          或
          <span className="login-alt-line" />
        </div>
        <button type="button" className="btn btn-ghost login-guest" onClick={actions.enterGuest}>
          游客模式 · 本地演示数据
        </button>

        <div className="login-foot">
          账户数据持久化于本地 PostgreSQL · <code>pnpm server</code>
        </div>
      </form>
    </div>
  );
}
