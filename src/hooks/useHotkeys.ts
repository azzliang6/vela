import { useEffect, useRef } from "react";
import { useMail } from "../store/MailContext";
import { FOLDERS } from "../types";

/**
 * 全局键盘快捷键（Gmail / Superhuman 风格）
 * j/k 上下切换 · e 归档 · s 星标 · r/a/f 回复/回复全部/转发 ·
 * c 写信 · # 删除 · / 搜索 · 1-6 切换文件夹 · t 主题 · Ctrl+K 命令面板
 */
export function useHotkeys() {
  const mail = useMail();
  const ref = useRef(mail);
  ref.current = mail;

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      const m = ref.current;
      const { state, actions, visibleEmails, selectedEmail } = m;
      const mod = ev.ctrlKey || ev.metaKey;
      const key = ev.key.toLowerCase();

      if (mod && key === "k") {
        ev.preventDefault();
        if (state.paletteOpen) actions.closePalette();
        else actions.openPalette();
        return;
      }

      if (ev.key === "Escape") {
        if (state.paletteOpen) actions.closePalette();
        else if (state.compose) actions.closeCompose(true);
        else if (state.checkedIds.length) actions.checksClear();
        return;
      }

      // Ctrl/Cmd+A：全选当前可见邮件（非输入场景）
      if (mod && key === "a") {
        if (state.compose || state.paletteOpen) return;
        const t = ev.target as HTMLElement | null;
        const inField =
          !!t &&
          (t.tagName === "INPUT" ||
            t.tagName === "TEXTAREA" ||
            t.tagName === "SELECT" ||
            t.isContentEditable);
        if (inField) return;
        ev.preventDefault();
        actions.checksSet(visibleEmails.map((e) => e.id), "replace");
        return;
      }

      // 弹层打开时不响应列表快捷键（面板内输入由面板自行处理）
      if (state.compose || state.paletteOpen) return;

      const t = ev.target as HTMLElement | null;
      const inField =
        !!t &&
        (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable);
      if (inField) {
        if (ev.key === "Escape") t.blur();
        return;
      }

      const idx = visibleEmails.findIndex((e) => e.id === state.selectedId);
      const select = (i: number) => {
        const target = visibleEmails[i];
        if (target) actions.select(target.id);
      };

      if (/^[1-6]$/.test(ev.key)) {
        const f = FOLDERS[Number(ev.key) - 1];
        if (f) actions.setView({ folder: f.id, category: null });
        return;
      }

      switch (key) {
        case "j":
          ev.preventDefault();
          select(idx < 0 ? 0 : Math.min(idx + 1, visibleEmails.length - 1));
          break;
        case "k":
          ev.preventDefault();
          select(idx < 0 ? visibleEmails.length - 1 : Math.max(idx - 1, 0));
          break;
        case "e":
          if (selectedEmail) {
            ev.preventDefault();
            actions.archive(selectedEmail.id);
            actions.toast("已归档 · 可在归档文件夹找到", "default");
          }
          break;
        case "s":
          if (selectedEmail) {
            ev.preventDefault();
            actions.toggleStar(selectedEmail.id);
          }
          break;
        case "r":
          if (selectedEmail) {
            ev.preventDefault();
            actions.replyTo(selectedEmail);
          }
          break;
        case "a":
          if (selectedEmail) {
            ev.preventDefault();
            actions.replyTo(selectedEmail, true);
          }
          break;
        case "f":
          if (selectedEmail) {
            ev.preventDefault();
            actions.forward(selectedEmail);
          }
          break;
        case "c":
          ev.preventDefault();
          actions.newCompose();
          break;
        case "#":
        case "delete":
        case "backspace":
          if (selectedEmail) {
            ev.preventDefault();
            actions.trash(selectedEmail.id);
            actions.toast("已移入垃圾箱", "default");
          }
          break;
        case "/":
          ev.preventDefault();
          document.getElementById("vela-search")?.focus();
          break;
        case "t":
          actions.toggleTheme();
          break;
        default:
          break;
      }
    };

    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}
