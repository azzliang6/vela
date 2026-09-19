import { MailProvider, useMail } from "./store/MailContext";
import { Sidebar } from "./components/Sidebar";
import { MailList } from "./components/MailList";
import { MailViewer } from "./components/MailViewer";
import { ComposeWindow } from "./components/ComposeWindow";
import { CommandPalette } from "./components/CommandPalette";
import { Toasts } from "./components/Toasts";
import { AddMailboxDialog } from "./components/AddMailboxDialog";
import { AISettingsDialog } from "./components/AISettingsDialog";
import { useHotkeys } from "./hooks/useHotkeys";
import { Menu, Sailboat } from "lucide-react";
import { useEffect, useState } from "react";

function Shell() {
  const { state } = useMail();
  useHotkeys();
  // 窄屏（手机）抽屉式侧边栏；桌面端 .side-drawer 为 display:contents，无副作用
  const [navOpen, setNavOpen] = useState(false);

  // 切换视图（账户/文件夹/分类）后自动收起抽屉
  useEffect(() => {
    setNavOpen(false);
  }, [state.folder, state.accountId, state.category]);

  if (!state.booted) {
    return (
      <div className="splash">
        <Sailboat className="sail" size={44} strokeWidth={1.2} />
        <span>正在恢复会话…</span>
      </div>
    );
  }

  return (
    <div className="app">
      <button
        className="icon-btn nav-toggle"
        aria-label="打开菜单"
        onClick={() => setNavOpen(true)}
      >
        <Menu size={18} />
      </button>
      <div className={`side-drawer ${navOpen ? "open" : ""}`}>
        <div className="drawer-backdrop" onClick={() => setNavOpen(false)} />
        <Sidebar />
      </div>
      <MailList />
      <MailViewer />
      {state.compose && <ComposeWindow />}
      {state.addMailboxOpen && <AddMailboxDialog />}
      {state.aiSettingsOpen && <AISettingsDialog />}
      {state.paletteOpen && <CommandPalette />}
      <Toasts />
    </div>
  );
}

export default function App() {
  return (
    <MailProvider>
      <Shell />
    </MailProvider>
  );
}
