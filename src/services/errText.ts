/**
 * 统一错误转文案。
 *
 * Tauri command 出错时 invoke 拒绝的是**纯字符串**（Rust 的 Err(String)），
 * 不是 Error 对象；HTTP 层拒绝的才是 Error。两种都要能读出消息，
 * 否则提示会变成「xxx：undefined」。
 */
export function errText(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "string") return e;
  try {
    return JSON.stringify(e);
  } catch {
    return String(e);
  }
}
