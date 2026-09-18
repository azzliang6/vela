import { useEffect, useState } from "react";
import { AlertTriangle, Check, Info } from "lucide-react";
import { useMail } from "../store/MailContext";
import type { Toast } from "../types";

const ICONS = {
  success: Check,
  warn: AlertTriangle,
  default: Info,
} as const;

export function Toasts() {
  const { state } = useMail();
  return (
    <div className="toasts">
      {state.toasts.map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </div>
  );
}

function ToastItem({ toast }: { toast: Toast }) {
  const { dispatch } = useMail();
  const [leaving, setLeaving] = useState(false);
  const Icon = ICONS[toast.tone];
  const ttl = toast.ttl ?? 2700;

  useEffect(() => {
    const t1 = window.setTimeout(() => setLeaving(true), ttl);
    const t2 = window.setTimeout(() => dispatch({ type: "toastPop", id: toast.id }), ttl + 350);
    return () => {
      window.clearTimeout(t1);
      window.clearTimeout(t2);
    };
    // 定时器只跟随 toast 本体，不随外部状态重置
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toast.id, ttl]);

  return (
    <div className={`toast ${toast.tone} ${leaving ? "leaving" : ""}`} role="status">
      <Icon size={15} />
      <span className="toast-text">{toast.text}</span>
      {toast.action && (
        <button
          className="toast-act"
          onClick={() => {
            toast.action!.run();
            dispatch({ type: "toastPop", id: toast.id });
          }}
        >
          {toast.action.label}
        </button>
      )}
    </div>
  );
}
