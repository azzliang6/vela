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
import { Menu, Minus, Sailboat, Square, X } from "lucide-react";
import { isTauri } from "@tauri-apps/api/core";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { useEffect, useState } from "react";

function WindowsTitlebar() {
  const appWindow = getCurrentWindow();

  return (
    <header
      className="window-titlebar"
      data-tauri-drag-region
      onDoubleClick={(event) => {
        if (!(event.target as HTMLElement).closest("button")) {
          void appWindow.toggleMaximize();
        }
      }}
    >
      <div className="window-title" data-tauri-drag-region>
        <Sailboat size={15} strokeWidth={1.8} aria-hidden="true" />
        <span>Vela 轻帆</span>
      </div>
      <div className="window-controls" aria-label="窗口控制">
        <button
          className="window-control"
          type="button"
          aria-label="最小化"
          title="最小化"
          onClick={() => void appWindow.minimize()}
        >
          <Minus size={15} strokeWidth={1.6} />
        </button>
        <button
          className="window-control"
          type="button"
          aria-label="最大化或还原"
          title="最大化或还原"
          onClick={() => void appWindow.toggleMaximize()}
        >
          <Square size={12} strokeWidth={1.6} />
        </button>
        <button
          className="window-control close"
          type="button"
          aria-label="关闭"
          title="关闭"
          onClick={() => void appWindow.close()}
        >
          <X size={16} strokeWidth={1.6} />
        </button>
      </div>
    </header>
  );
}

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
  const useWindowsFrame = isTauri() && /Windows/i.test(navigator.userAgent);

  return (
    <div className={useWindowsFrame ? "windows-frame" : undefined}>
      {useWindowsFrame && <WindowsTitlebar />}
      <MailProvider>
        <Shell />
      </MailProvider>
    </div>
  );
}
