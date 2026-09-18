//! SMTP 发信。
//!
//! 用 lettre 构造标准 MIME 邮件并通过 SMTP 投递。
//! - 465 端口用隐式 TLS（SMTPS）
//! - 587 / 25 端口用 STARTTLS（强制要求加密，失败即中止，避免凭证明文外泄）
//!
//! 发信成功后，本客户端不会主动去 IMAP 的「已发送」里补一条记录：
//! 大多数邮箱服务器（QQ / 163 / Gmail / Outlook）在通过 SMTP 发信后会自动归档到已发送，
//! 下次同步即可拉到；重复写入反而会产生两封一模一样的邮件。

use lettre::message::header::ContentType;
use lettre::message::{Mailbox, Message};
use lettre::transport::smtp::authentication::Credentials;
use lettre::transport::smtp::client::{Tls, TlsParameters};
use lettre::{SmtpTransport, Transport};

use super::providers::{MailServerConfig, Security};

/// 待发送的邮件内容
pub struct OutgoingMail {
    /// 发件人显示名（当前由账户邮箱前缀派生；保留字段以便将来支持自定义签名）
    #[allow(dead_code)]
    pub from_name: String,
    /// 收件人，"姓名 <邮箱>" 或纯邮箱
    pub to: Vec<String>,
    pub cc: Vec<String>,
    /// 回复时用于串起会话（保留原邮件 Message-ID）
    pub in_reply_to: Option<String>,
    pub subject: String,
    pub body: String,
}

/// 把 "张三 <a@b.com>" 或 "a@b.com" 解析成 lettre 的 Mailbox
fn parse_mailbox(raw: &str) -> Result<Mailbox, String> {
    let s = raw.trim();
    if s.is_empty() {
        return Err("收件人为空".into());
    }

    // 形如 「姓名 <邮箱>」
    if let (Some(l), Some(r)) = (s.find('<'), s.rfind('>')) {
        if r > l {
            let name = s[..l].trim().trim_matches('"').to_string();
            let addr = s[l + 1..r].trim();
            let addr = addr
                .parse()
                .map_err(|_| format!("邮箱地址不合法：{addr}"))?;
            return Ok(Mailbox::new(
                if name.is_empty() { None } else { Some(name) },
                addr,
            ));
        }
    }

    // 纯邮箱
    let addr = s.parse().map_err(|_| format!("邮箱地址不合法：{s}"))?;
    Ok(Mailbox::new(None, addr))
}

/// 构建 SMTP 传输通道
fn transport(cfg: &MailServerConfig) -> Result<SmtpTransport, String> {
    let params = TlsParameters::new(cfg.smtp_host.clone())
        .map_err(|e| format!("TLS 参数初始化失败：{e}"))?;

    let tls = match cfg.smtp_security {
        Security::Ssl => Tls::Wrapper(params),
        // Required = 必须成功升级到 TLS，否则放弃连接（防止降级攻击）
        Security::Starttls => Tls::Required(params),
        Security::None => {
            return Err("出于安全考虑不支持无加密的 SMTP；请使用 465(SSL) 或 587(STARTTLS)".into())
        }
    };

    let creds = Credentials::new(cfg.email.clone(), cfg.password.clone());

    Ok(SmtpTransport::builder_dangerous(cfg.smtp_host.as_str())
        .port(cfg.smtp_port)
        .tls(tls)
        .credentials(creds)
        .timeout(Some(std::time::Duration::from_secs(60)))
        .build())
}

/// 发送一封邮件。成功返回服务器接受的信息。
pub fn send(cfg: &MailServerConfig, mail: &OutgoingMail) -> Result<String, String> {
    let from = parse_mailbox(&format!("{} <{}>", cfg.from_display_name(), cfg.email))?;

    let to: Vec<Mailbox> = mail
        .to
        .iter()
        .map(|s| parse_mailbox(s))
        .collect::<Result<_, _>>()?;
    if to.is_empty() {
        return Err("请至少填写一个收件人".into());
    }

    let mut builder = Message::builder()
        .from(from)
        .subject(mail.subject.clone());

    for m in &to {
        builder = builder.to(m.clone());
    }
    for raw in &mail.cc {
        builder = builder.cc(parse_mailbox(raw)?);
    }
    if let Some(orig) = &mail.in_reply_to {
        // In-Reply-To 必须是原始邮件带尖括号的 Message-ID，缺失尖括号时补上
        let id = if orig.trim_start().starts_with('<') {
            orig.clone()
        } else {
            format!("<{orig}>")
        };
        builder = builder.in_reply_to(id);
    }

    let message: Message = builder
        .header(ContentType::TEXT_PLAIN)
        // 声明用 UTF-8，避免中文正文在部分客户端乱码
        .body(mail.body.clone())
        .map_err(|e| format!("构造邮件失败：{e}"))?;

    let mailer = transport(cfg)?;
    mailer
        .send(&message)
        .map(|resp| {
            let text = format!("{resp:?}");
            if text.is_empty() {
                "已投递".to_string()
            } else {
                text
            }
        })
        .map_err(|e| smtp_error(&e.to_string()))
}

/// 把 SMTP 错误翻译成用户可理解的提示
fn smtp_error(text: &str) -> String {
    let lower = text.to_ascii_lowercase();
    if lower.contains("auth") || lower.contains("535") || lower.contains("credentials") {
        return format!("SMTP 认证失败：请确认使用的是授权码 / 应用专用密码（{text}）");
    }
    if lower.contains("relay") || lower.contains("550") {
        return format!("服务器拒绝投递：{text}（可能被判定为垃圾邮件或收件人不存在）");
    }
    format!("发送失败：{text}")
}

impl MailServerConfig {
    /// 发件人显示名：没有名字时直接用邮箱地址
    fn from_display_name(&self) -> String {
        self.email
            .split('@')
            .next()
            .unwrap_or(&self.email)
            .to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_plain_address() {
        let m = parse_mailbox("a@b.com").unwrap();
        assert_eq!(m.email.to_string(), "a@b.com");
        assert!(m.name.is_none());
    }

    #[test]
    fn parse_named_address() {
        let m = parse_mailbox("张三 <a@b.com>").unwrap();
        assert_eq!(m.email.to_string(), "a@b.com");
        assert_eq!(m.name.as_deref(), Some("张三"));
    }

    #[test]
    fn parse_quoted_name() {
        let m = parse_mailbox("\"Doe, John\" <j@b.com>").unwrap();
        assert_eq!(m.email.to_string(), "j@b.com");
        assert_eq!(m.name.as_deref(), Some("Doe, John"));
    }

    #[test]
    fn parse_rejects_empty_and_invalid() {
        assert!(parse_mailbox("").is_err());
        assert!(parse_mailbox("   ").is_err());
        assert!(parse_mailbox("not an email").is_err());
        assert!(parse_mailbox("名字 <>").is_err());
    }

    #[test]
    fn parse_trims_whitespace() {
        let m = parse_mailbox("  a@b.com  ").unwrap();
        assert_eq!(m.email.to_string(), "a@b.com");
    }

    #[test]
    fn unencrypted_smtp_rejected() {
        let cfg = MailServerConfig {
            provider: "custom".into(),
            email: "a@b.com".into(),
            imap_host: "mail.b.com".into(),
            imap_port: 993,
            imap_security: Security::Ssl,
            smtp_host: "mail.b.com".into(),
            smtp_port: 25,
            smtp_security: Security::None,
            password: "x".into(),
        };
        let err = transport(&cfg).unwrap_err();
        assert!(err.contains("不支持无加密"), "实际：{err}");
    }

    #[test]
    fn missing_recipient_rejected() {
        let cfg = MailServerConfig {
            provider: "qq".into(),
            email: "a@qq.com".into(),
            imap_host: "imap.qq.com".into(),
            imap_port: 993,
            imap_security: Security::Ssl,
            smtp_host: "smtp.qq.com".into(),
            smtp_port: 465,
            smtp_security: Security::Ssl,
            password: "x".into(),
        };
        let mail = OutgoingMail {
            from_name: "我".into(),
            to: vec![],
            cc: vec![],
            in_reply_to: None,
            subject: "主题".into(),
            body: "正文".into(),
        };
        // 收件人为空必须在连服务器之前就被拦下
        let err = send(&cfg, &mail).unwrap_err();
        assert!(err.contains("收件人"), "实际：{err}");
    }
}
