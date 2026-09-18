import { MailProvider, useMail } from "./store/MailContext";
import { Sidebar } from "./components/Sidebar";
import { MailList } from "./components/MailList";
import { MailViewer } from "./components/MailViewer";
import { ComposeWindow } from "./components/ComposeWindow";
import { CommandPalette } from "./components/CommandPalette";
import { Toasts } from "./components/Toasts";
import { LoginScreen } from "./components/LoginScreen";
import { AddMailboxDialog } from "./components/AddMailboxDialog";
import { useHotkeys } from "./hooks/useHotkeys";
import { Sailboat } from "lucide-react";

function Shell() {
  const { state } = useMail();
  useHotkeys();

  if (!state.booted) {
    return (
      <div className="splash">
        <Sailboat className="sail" size={44} strokeWidth={1.2} />
        <span>正在恢复会话…</span>
      </div>
    );
  }

  if (!state.auth) {
    return (
      <>
        <LoginScreen />
        <Toasts />
      </>
    );
  }

  return (
    <div className="app">
      <Sidebar />
      <MailList />
      <MailViewer />
      {state.compose && <ComposeWindow />}
      {state.addMailboxOpen && <AddMailboxDialog />}
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
