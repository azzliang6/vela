/**
 * AI 分析服务：真实 LLM 调用 + 设置管理。
 *
 * - 任意 OpenAI 兼容接口均可（智谱 GLM / DeepSeek / OpenAI / 本地 Ollama…），
 *   HTTP 在 Rust 侧完成（WebView fetch 会被 CORS 拦截）
 * - 提示词模板完全由用户自定义，设置面板提供内置默认模板
 *   （含输出语言、分类枚举、输出 JSON 格式约定）
 * - 模板支持占位符：{{language}} {{subject}} {{from_name}} {{from_email}} {{body}}
 */

import type { Email, SmartCategory } from "../types";
import { isDesktop } from "./realMailService";

/* ———— 服务商预设 ———— */

export interface AiProviderPreset {
  id: string;
  label: string;
  baseUrl: string;
  model: string;
  hint: string;
}

export const AI_PRESETS: AiProviderPreset[] = [
  {
    id: "zhipu",
    label: "智谱 GLM",
    baseUrl: "https://open.bigmodel.cn/api/paas/v4",
    model: "glm-4-flash",
    hint: "glm-4-flash 免费且有新用户额度；API Key 在 open.bigmodel.cn 的「API Keys」页面创建",
  },
  {
    id: "deepseek",
    label: "DeepSeek",
    baseUrl: "https://api.deepseek.com/v1",
    model: "deepseek-chat",
    hint: "API Key 在 platform.deepseek.com 创建",
  },
  {
    id: "openai",
    label: "OpenAI",
    baseUrl: "https://api.openai.com/v1",
    model: "gpt-4o-mini",
    hint: "API Key 在 platform.openai.com 创建；国内网络需自行保证可达",
  },
  {
    id: "custom",
    label: "自定义（OpenAI 兼容）",
    baseUrl: "",
    model: "",
    hint: "任何兼容 chat/completions 的服务，如本地 Ollama（http://127.0.0.1:11434/v1）",
  },
];

/* ———— 默认提示词模板 ———— */

export const AI_LANGUAGES = ["简体中文", "English", "繁體中文", "日本語"] as const;

/** 正文超过此长度截断，控制 token 开销 */
const MAX_BODY_CHARS = 6000;

export const DEFAULT_PROMPT_TEMPLATE = `你是一位高效的邮件助手，请分析下面这封邮件，输出分类、一句话摘要和重要性说明。

要求：
- 输出语言：{{language}}
- summary：概括这封邮件最重要的内容，不超过 80 字
- why：仅当 category 为 important 时给出一句话理由，其余情况填空字符串
- category 必须从以下固定值中选择一个：
  important（重要/需要行动） finance（账单/财务） travel（出行/行程）
  social（社交/生活） newsletter（订阅/推广） notification（系统通知） spam（垃圾邮件）

只输出一个 JSON 对象，不要包含任何其他文字，格式：
{"category":"important","summary":"…","why":"…"}

邮件内容：
发件人：{{from_name}} <{{from_email}}>
主题：{{subject}}
正文：
{{body}}`;

/* ———— 设置存取 ———— */

export interface AiSettings {
  providerId: string;
  baseUrl: string;
  apiKey: string;
  /** 各厂商分别记忆的 Key：切换厂商时自动带出对应的，避免残留别家的 Key */
  apiKeys: Record<string, string>;
  model: string;
  /** 摘要输出语言，会替换模板中的 {{language}} */
  language: string;
  promptTemplate: string;
  /** 同步后自动分析最近 3 天的新邮件 */
  autoAnalyze: boolean;
  /** 自动分析的扫描间隔（分钟）；0 = 每次有新邮件立即分析 */
  autoAnalyzeIntervalMin: number;
}

/** 自动分析频率的可选值（分钟；0 表示紧跟新邮件） */
export const AUTO_ANALYZE_INTERVALS: { value: number; label: string }[] = [
  { value: 0, label: "紧跟新邮件（默认）" },
  { value: 5, label: "每 5 分钟扫描一次" },
  { value: 15, label: "每 15 分钟扫描一次" },
  { value: 60, label: "每小时扫描一次" },
];

const LS_KEY = "vela:ai-settings-v1";

export function defaultAiSettings(): AiSettings {
  const p = AI_PRESETS[0];
  return {
    providerId: p.id,
    baseUrl: p.baseUrl,
    apiKey: "",
    apiKeys: {},
    model: p.model,
    language: "简体中文",
    promptTemplate: DEFAULT_PROMPT_TEMPLATE,
    autoAnalyze: true,
    autoAnalyzeIntervalMin: 0,
  };
}

export function loadAiSettings(): AiSettings {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (!raw) return defaultAiSettings();
    const s = { ...defaultAiSettings(), ...(JSON.parse(raw) as Partial<AiSettings>) };
    s.apiKeys = { ...s.apiKeys };
    // 迁移：旧版本只有单个 apiKey 字段，视作当前厂商的 Key
    if (s.apiKey && !s.apiKeys[s.providerId]) s.apiKeys[s.providerId] = s.apiKey;
    return s;
  } catch {
    return defaultAiSettings();
  }
}

export function saveAiSettings(s: AiSettings): void {
  try {
    localStorage.setItem(LS_KEY, JSON.stringify(s));
  } catch {
    /* ignore */
  }
}

/** 是否已配置到可真实调用（其余情况 analyze 走本地规则兜底） */
export function aiConfigured(s: AiSettings): boolean {
  return isDesktop() && !!s.baseUrl.trim() && !!s.apiKey.trim() && !!s.model.trim();
}

/* ———— 提示词渲染 ———— */

export function renderPrompt(template: string, email: Email, language: string): string {
  const body =
    email.body.length > MAX_BODY_CHARS
      ? `${email.body.slice(0, MAX_BODY_CHARS)}\n…（正文过长已截断）`
      : email.body;
  return template
    .replaceAll("{{language}}", language)
    .replaceAll("{{subject}}", email.subject || "（无主题）")
    .replaceAll("{{from_name}}", email.from.name)
    .replaceAll("{{from_email}}", email.from.email)
    .replaceAll("{{body}}", body);
}

/* ———— 调用与解析 ———— */

export interface AiAnalysisResult {
  category: SmartCategory | null;
  summary: string;
  why?: string;
}

const VALID_CATEGORIES: SmartCategory[] = [
  "important",
  "finance",
  "travel",
  "social",
  "newsletter",
  "notification",
  "spam",
];

async function invokeChat(prompt: string, settings: AiSettings): Promise<string> {
  const mod = await import("@tauri-apps/api/core");
  return mod.invoke<string>("ai_chat", {
    input: {
      baseUrl: settings.baseUrl.trim(),
      apiKey: settings.apiKey.trim(),
      model: settings.model.trim(),
      prompt,
    },
  });
}

/** 分析一封邮件：返回结构化结果。 */
export async function analyzeWithAi(
  email: Email,
  settings: AiSettings,
): Promise<AiAnalysisResult> {
  const prompt = renderPrompt(settings.promptTemplate, email, settings.language);
  const text = await invokeChat(prompt, settings);
  return parseAnalysis(text);
}

/**
 * 宽松解析模型输出：容忍 ```json 围栏、前后闲话；
 * 都解析不出来时把原文当作摘要，保证用户至少能看到模型说了什么。
 */
export function parseAnalysis(text: string): AiAnalysisResult {
  const fenced = text.match(/```(?:json)?\s*([\s\S]*?)```/);
  const candidate = fenced?.[1] ?? text;
  const start = candidate.indexOf("{");
  const end = candidate.lastIndexOf("}");
  if (start !== -1 && end > start) {
    try {
      const obj = JSON.parse(candidate.slice(start, end + 1)) as {
        category?: unknown;
        summary?: unknown;
        why?: unknown;
      };
      const category = VALID_CATEGORIES.includes(obj.category as SmartCategory)
        ? (obj.category as SmartCategory)
        : null;
      const summary = typeof obj.summary === "string" && obj.summary.trim() ? obj.summary.trim() : "";
      const why = typeof obj.why === "string" && obj.why.trim() ? obj.why.trim() : undefined;
      if (summary || category) return { category, summary, why };
    } catch {
      /* 落到下面的兜底 */
    }
  }
  return { category: null, summary: text.slice(0, 200), why: undefined };
}

/** 测试当前配置是否可用：发一句极短的对话，返回模型回复 */
export async function testAiSettings(settings: AiSettings): Promise<string> {
  const mod = await import("@tauri-apps/api/core");
  return mod.invoke<string>("ai_chat", {
    input: {
      baseUrl: settings.baseUrl.trim(),
      apiKey: settings.apiKey.trim(),
      model: settings.model.trim(),
      prompt: "请只回复两个字：你好",
    },
  });
}
