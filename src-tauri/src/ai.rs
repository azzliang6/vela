//! AI 分析：调用 OpenAI 兼容的 chat/completions 接口。
//!
//! 只做一个最通用的「发 prompt、回文本」命令，不在 Rust 侧拼提示词——
//! 提示词模板、输出语言等全部由前端设置面板管理，这里保持无状态，
//! 换服务商（智谱 / DeepSeek / OpenAI / 自建）不用改这边的代码。
//!
//! 走 Rust 而不是 WebView fetch，是因为远程 API 普遍不带 CORS 头，
//! WebView 里直接 fetch 会被浏览器安全策略拦下。

use serde::Deserialize;
use serde_json::Value;
use std::time::Duration;

type CmdResult<T> = Result<T, String>;

/// 前端传入的一次对话参数
#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AiChatInput {
    /// OpenAI 兼容服务的根地址，如 https://open.bigmodel.cn/api/paas/v4
    pub base_url: String,
    pub api_key: String,
    pub model: String,
    /// 已渲染好的完整用户提示词
    pub prompt: String,
}

/// 调用 chat/completions，返回模型输出的第一段文本。
#[tauri::command]
pub async fn ai_chat(input: AiChatInput) -> CmdResult<String> {
    tauri::async_runtime::spawn_blocking(move || chat_blocking(&input))
        .await
        .map_err(|e| format!("任务执行失败：{e}"))?
}

fn chat_blocking(input: &AiChatInput) -> CmdResult<String> {
    let base = input.base_url.trim().trim_end_matches('/');
    if base.is_empty() {
        return Err("未配置 API 地址".into());
    }
    if input.api_key.trim().is_empty() {
        return Err("未配置 API Key".into());
    }
    let url = format!("{base}/chat/completions");

    // 连接 15s / 整体 90s：LLM 生成长摘要可能比普通接口慢。
    // ureq 的 "native-tls" 特性只是适配器，必须手动注入 TlsConnector，否则无 TLS 后端
    // （表现为 Unknown Scheme: no TLS backend is configured）。
    let tls = std::sync::Arc::new(
        native_tls::TlsConnector::builder()
            .build()
            .map_err(|e| format!("TLS 初始化失败：{e}"))?,
    );
    let agent = ureq::AgentBuilder::new()
        .tls_connector(tls)
        .timeout_connect(Duration::from_secs(15))
        .timeout(Duration::from_secs(90))
        .build();

    let body = serde_json::json!({
        "model": input.model,
        "messages": [
            { "role": "user", "content": input.prompt },
        ],
        "temperature": 0.2,
        "stream": false,
    });

    let resp = agent
        .post(&url)
        .set("Authorization", &format!("Bearer {}", input.api_key.trim()))
        .set("Content-Type", "application/json")
        .send_json(body)
        .map_err(|e| map_transport_error(e, &url))?;

    let status = resp.status();
    if status != 200 {
        let detail = resp
            .into_string()
            .unwrap_or_default();
        return Err(format!("API 返回 {status}：{}", brief_error(&detail)));
    }

    let parsed: Value = resp
        .into_json()
        .map_err(|e| format!("响应不是合法 JSON：{e}"))?;

    parsed
        .pointer("/choices/0/message/content")
        .and_then(|v| v.as_str())
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .ok_or_else(|| "响应里没有模型输出（choices[0].message.content 为空）".to_string())
}

/// 把 ureq 的传输错误翻成用户能看懂的提示（重点区分网络不通与鉴权失败）
fn map_transport_error(e: ureq::Error, url: &str) -> String {
    match e {
        ureq::Error::Status(code, resp) => {
            let detail = resp.into_string().unwrap_or_default();
            let hint = match code {
                401 => "API Key 无效或已过期",
                403 => "没有权限（Key 未开通该模型？）",
                404 => "接口地址不对（检查 API 地址是否为 OpenAI 兼容根路径）",
                429 => "请求过于频繁或额度不足",
                _ => "",
            };
            let prefix = if hint.is_empty() { String::new() } else { format!("（{hint}）") };
            format!("API 返回 {code}{prefix}：{}", brief_error(&detail))
        }
        other => format!("无法连接 {url}：{other}"),
    }
}

/// 从 API 的 JSON 错误体里抽出最短的可读信息，避免整段透传
fn brief_error(body: &str) -> String {
    let v: Value = match serde_json::from_str(body) {
        Ok(v) => v,
        Err(_) => return truncate(body, 300),
    };
    let msg = v
        .pointer("/error/message")
        .or_else(|| v.pointer("/message"))
        .or_else(|| v.pointer("/error"))
        .and_then(|x| x.as_str())
        .unwrap_or(body);
    truncate(msg, 300)
}

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        return s.to_string();
    }
    let cut: String = s.chars().take(max).collect();
    format!("{cut}…")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn brief_error_extracts_message() {
        let body = r#"{"error":{"message":"Invalid API key","code":1001}}"#;
        assert_eq!(brief_error(body), "Invalid API key");
    }

    #[test]
    fn brief_error_falls_back_to_raw() {
        assert_eq!(brief_error("plain text"), "plain text");
    }
}
