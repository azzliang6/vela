import type { Email, SmartCategory } from "../types";

/**
 * 本地规则分类引擎（离线可用，零依赖）。
 * 为真实邮箱接入前的分类/摘要兜底：按发件人特征与关键词给出可解释的分类依据。
 * 未来接 LLM API 或 Rust 侧 IMAP 分类时，只需替换本文件实现。
 */

export interface RuleResult {
  category: SmartCategory;
  summary: string;
  why?: string;
}

const SPAM_HINTS = ["中奖", "抢购资格", "代开", "点击领取", "退订回td", "点数低至", "仅需支付", "名额有限"];
const SPAM_DOMAIN = [".win", ".top", ".xyz", ".club", ".vip", "lucky-star", "vip-163"];
const FINANCE = ["账单", "还款", "信用卡", "发票", "报销", "续费", "付款", "工资", "到账", "扣款", "退款", "免息", "分期"];
const TRAVEL = ["航班", "值机", "行程", "车次", "出票", "登机", "座位", "起飞", "到达", "酒店", "高铁", "火车"];
const SOCIAL = ["爬山", "聚会", "聚餐", "夜爬", "周末", "回归礼包", "新赛季", "皮肤", "点赞"];
const NEWSLETTER = ["newsletter", "周刊", "周报", "日报", "digest", "退订", "降价", "促销", "折扣", "购物车"];
const NOTIFICATION = ["通知", "提醒", "待审批", "审批", "部署", "扩容", "取件码", "派送", "已送达", "到期", "释放", "发放"];
const IMPORTANT_TIME = ["本周", "明早", "明天", "截止", "周三前", "周五前", "今天", "尽快", "小时"];
const IMPORTANT_ACTION = ["务必", "需要你", "给我", "指派", "请于", "请查收", "收下", "立即", "带上", "排期"];

function hits(text: string, words: string[]): string[] {
  return words.filter((w) => text.includes(w));
}

/** 按规则给出分类；无任何命中时返回 null（保持未分类） */
export function classifyRules(email: Email): SmartCategory | null {
  const hay = `${email.subject}\n${email.from.email}\n${email.from.name}\n${email.body}`.toLowerCase();
  const domain = (email.from.email.split("@")[1] ?? "").toLowerCase();

  if (hits(hay, SPAM_HINTS).length > 0 || SPAM_DOMAIN.some((d) => domain.includes(d))) {
    return "spam";
  }
  if (hits(hay, FINANCE).length > 0) return "finance";
  if (hits(hay, TRAVEL).length > 0) return "travel";

  const timeHits = hits(hay, IMPORTANT_TIME);
  const actionHits = hits(hay, IMPORTANT_ACTION);
  if (timeHits.length + actionHits.length >= 2) return "important";

  if (hits(hay, NEWSLETTER).length > 0) return "newsletter";
  if (hits(hay, SOCIAL).length > 0) return "social";
  if (hits(hay, NOTIFICATION).length > 0) return "notification";
  return null;
}

/** 抽取式摘要：挑信息量最高的一两句（数字/金额/关键动词加权，跳过问候与落款） */
export function summarizeRules(email: Email): string {
  const SKIP = /^(尊敬的|你好|您好|各位|亲爱的|晚晴|陈墨|航航|张总|楠哥|一然|祝|此致|——|·|【|召唤师|编辑导读)/;
  const KEY = /(¥|\d|定档|截止|需要|务必|请|已|成功|出票|账单|发布|安排|提醒|到|将|总计|金额)/;

  const sentences = email.body
    .split(/[\n。！？!?]+/)
    .map((s) => s.trim())
    .filter((s) => s.length >= 8 && s.length <= 90);

  let best = "";
  let bestScore = -Infinity;
  for (const s of sentences) {
    let score = 0;
    if (KEY.test(s)) score += 3;
    if (/\d/.test(s)) score += 1;
    if (SKIP.test(s)) score -= 5;
    if (score > bestScore) {
      bestScore = score;
      best = s;
    }
  }
  if (!best) best = sentences[0] ?? email.subject;
  best = best.replace(/^【[^】]*】/, "").trim();
  return best.length > 64 ? `${best.slice(0, 63)}…` : best;
}

/** 完整分析：分类 + 摘要 + 重要邮件的可解释依据 */
export function analyzeEmail(email: Email): RuleResult | null {
  const category = classifyRules(email);
  const summary = summarizeRules(email);
  if (!category) return null;

  let why: string | undefined;
  if (category === "important") {
    const hay = `${email.subject}\n${email.body}`.toLowerCase();
    const found = [...hits(hay, IMPORTANT_TIME).slice(0, 2), ...hits(hay, IMPORTANT_ACTION).slice(0, 2)];
    if (found.length) why = `命中信号：${Array.from(new Set(found)).join("、")}`;
  }
  return { category, summary, why };
}
