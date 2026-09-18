//! 邮箱服务商预设。
//!
//! 用户选一个服务商，前端自动填好 IMAP/SMTP 服务器地址与端口，只需再填邮箱和授权码。
//! `custom` 允许手动指定全部参数（自建邮件服务器、企业邮箱等）。
//!
//! 端口约定：
//! - IMAP 993 = 隐式 TLS（连接即加密，推荐）
//! - IMAP 143 = STARTTLS（先明文再用 STARTTLS 升级）
//! - SMTP 465 = 隐式 TLS，587/25 = STARTTLS

use serde::{Deserialize, Serialize};

/// 传输加密方式
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Security {
    /// 隐式 TLS（SSL/TLS，连上就是加密的）
    Ssl,
    /// STARTTLS（先明文连接，再升级加密）
    Starttls,
    /// 不加密（仅建议用于本机自建服务器）
    None,
}

/// 一个完整邮箱账户的连接参数（不含密码）
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MailServerConfig {
    /// 服务商标识，如 "qq"、"gmail"、"custom"
    pub provider: String,
    /// 邮箱地址，同时作为 IMAP/SMTP 用户名
    pub email: String,
    pub imap_host: String,
    pub imap_port: u16,
    pub imap_security: Security,
    pub smtp_host: String,
    pub smtp_port: u16,
    pub smtp_security: Security,
    /// 授权码 / 密码（仅内存中短暂存在，落库前会加密）
    #[serde(default, skip_serializing)]
    pub password: String,
}

/// 前端展示用的服务商预设（不含密码）
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProviderPreset {
    pub id: String,
    pub label: String,
    pub imap_host: String,
    pub imap_port: u16,
    pub smtp_host: String,
    pub smtp_port: u16,
    /// 该服务商申请授权码的提示
    pub hint: String,
    /// 邮箱域名后缀，用于用户输入时自动匹配服务商
    pub domains: Vec<String>,
}

struct Preset {
    id: &'static str,
    label: &'static str,
    imap_host: &'static str,
    imap_port: u16,
    smtp_host: &'static str,
    smtp_port: u16,
    hint: &'static str,
    domains: &'static [&'static str],
}

/// 国内 + 国际常见邮箱预设。全部使用「隐式 TLS + 993/465」，这是最省心也最安全的组合。
const PRESETS: &[Preset] = &[
    Preset {
        id: "qq",
        label: "QQ 邮箱",
        imap_host: "imap.qq.com",
        imap_port: 993,
        smtp_host: "smtp.qq.com",
        smtp_port: 465,
        hint: "QQ 邮箱需在「设置 → 账户 → POP3/IMAP/SMTP 服务」开启 IMAP 并生成 16 位授权码，登录时用它代替 QQ 密码",
        domains: &["qq.com", "vip.qq.com", "foxmail.com"],
    },
    Preset {
        id: "163",
        label: "网易 163",
        imap_host: "imap.163.com",
        imap_port: 993,
        smtp_host: "smtp.163.com",
        smtp_port: 465,
        hint: "163 邮箱在「设置 → POP3/SMTP/IMAP」开启 IMAP/SMTP 后生成授权码；首次连接可能需在网页端确认客户端授权",
        domains: &["163.com"],
    },
    Preset {
        id: "126",
        label: "网易 126",
        imap_host: "imap.126.com",
        imap_port: 993,
        smtp_host: "smtp.126.com",
        smtp_port: 465,
        hint: "126 邮箱与 163 相同，需开启 IMAP/SMTP 并使用授权码登录",
        domains: &["126.com"],
    },
    Preset {
        id: "yeah",
        label: "网易 yeah.net",
        imap_host: "imap.yeah.net",
        imap_port: 993,
        smtp_host: "smtp.yeah.net",
        smtp_port: 465,
        hint: "yeah.net 邮箱需开启 IMAP/SMTP 并使用授权码登录",
        domains: &["yeah.net"],
    },
    Preset {
        id: "gmail",
        label: "Gmail",
        imap_host: "imap.gmail.com",
        imap_port: 993,
        smtp_host: "smtp.gmail.com",
        smtp_port: 465,
        hint: "Gmail 需开启两步验证后生成「应用专用密码」，用它作为密码；普通账号密码无法通过 IMAP 登录。国内网络需能访问 Google 服务",
        domains: &["gmail.com", "googlemail.com"],
    },
    Preset {
        id: "outlook",
        label: "Outlook / Hotmail",
        imap_host: "outlook.office365.com",
        imap_port: 993,
        smtp_host: "smtp.office365.com",
        smtp_port: 587,
        hint: "Outlook 个人账户可直接用账户密码；若开启了双重验证，需生成「应用密码」。企业账户需管理员开启 IMAP",
        domains: &[
            "outlook.com",
            "hotmail.com",
            "live.com",
            "msn.com",
            "outlook.jp",
        ],
    },
    Preset {
        id: "icloud",
        label: "iCloud",
        imap_host: "imap.mail.me.com",
        imap_port: 993,
        smtp_host: "smtp.mail.me.com",
        smtp_port: 587,
        hint: "iCloud 需在 Apple ID 设置中生成「App 专用密码」，账号名填完整邮箱地址",
        domains: &["icloud.com", "me.com", "mac.com"],
    },
    Preset {
        id: "yahoo",
        label: "Yahoo 邮箱",
        imap_host: "imap.mail.yahoo.com",
        imap_port: 993,
        smtp_host: "smtp.mail.yahoo.com",
        smtp_port: 465,
        hint: "Yahoo 需在账户安全设置中生成「应用密码」",
        domains: &["yahoo.com", "yahoo.co.jp", "ymail.com"],
    },
    Preset {
        id: "exmail",
        label: "腾讯企业邮箱",
        imap_host: "imap.exmail.qq.com",
        imap_port: 993,
        smtp_host: "smtp.exmail.qq.com",
        smtp_port: 465,
        hint: "企业邮箱通常可直接用登录密码，若管理员开启了安全设置则需在网页端生成专用密码",
        domains: &["exmail.qq.com"],
    },
    Preset {
        id: "aliyun",
        label: "阿里云邮箱",
        imap_host: "imap.mxhichina.com",
        imap_port: 993,
        smtp_host: "smtp.mxhichina.com",
        smtp_port: 465,
        hint: "阿里云企业邮箱使用完整邮箱地址作为用户名，密码为邮箱登录密码",
        domains: &["mxhichina.com", "aliyun.com"],
    },
    Preset {
        id: "sina",
        label: "新浪邮箱",
        imap_host: "imap.sina.com",
        imap_port: 993,
        smtp_host: "smtp.sina.com",
        smtp_port: 465,
        hint: "新浪邮箱需在「设置 → 客户端 POP/IMAP」中开启 IMAP 服务",
        domains: &["sina.com", "sina.cn"],
    },
    Preset {
        id: "custom",
        label: "自定义 / 自建服务器",
        imap_host: "",
        imap_port: 993,
        smtp_host: "",
        smtp_port: 465,
        hint: "手动填写 IMAP 与 SMTP 服务器地址。常见约定：IMAP 993(SSL) 或 143(STARTTLS)；SMTP 465(SSL) 或 587(STARTTLS)",
        domains: &[],
    },
];

/// 全部预设，供前端渲染下拉框
pub fn all_presets() -> Vec<ProviderPreset> {
    PRESETS
        .iter()
        .map(|p| ProviderPreset {
            id: p.id.to_string(),
            label: p.label.to_string(),
            imap_host: p.imap_host.to_string(),
            imap_port: p.imap_port,
            smtp_host: p.smtp_host.to_string(),
            smtp_port: p.smtp_port,
            hint: p.hint.to_string(),
            domains: p.domains.iter().map(|d| d.to_string()).collect(),
        })
        .collect()
}

/// 根据邮箱地址猜测服务商；匹配不到返回 "custom"
pub fn guess_provider(email: &str) -> String {
    let domain = email.rsplit_once('@').map(|(_, d)| d).unwrap_or("").to_ascii_lowercase();
    for p in PRESETS {
        if p.domains.contains(&domain.as_str()) {
            return p.id.to_string();
        }
    }
    "custom".to_string()
}

/// 用预设补齐服务器地址。`provider == "custom"` 时保留前端传来的 host/port。
pub fn apply_preset(cfg: &mut MailServerConfig) -> Result<(), String> {
    if cfg.email.trim().is_empty() || !cfg.email.contains('@') {
        return Err("请填写完整的邮箱地址".into());
    }
    let Some(p) = PRESETS.iter().find(|p| p.id == cfg.provider) else {
        return Err(format!("未知的服务商：{}", cfg.provider));
    };
    if p.id == "custom" {
        if cfg.imap_host.trim().is_empty() || cfg.smtp_host.trim().is_empty() {
            return Err("自定义服务商需要填写 IMAP 与 SMTP 服务器地址".into());
        }
    } else {
        cfg.imap_host = p.imap_host.to_string();
        cfg.imap_port = p.imap_port;
        cfg.smtp_host = p.smtp_host.to_string();
        cfg.smtp_port = p.smtp_port;
    }
    // 993 / 465 用隐式 TLS；143 / 587 / 25 用 STARTTLS
    cfg.imap_security = if cfg.imap_port == 993 {
        Security::Ssl
    } else if cfg.imap_port == 143 {
        Security::Starttls
    } else {
        cfg.imap_security
    };
    cfg.smtp_security = if cfg.smtp_port == 465 {
        Security::Ssl
    } else if cfg.smtp_port == 587 || cfg.smtp_port == 25 {
        Security::Starttls
    } else {
        cfg.smtp_security
    };
    Ok(())
}

/// 常见邮箱的服务端文件夹名候选。
///
/// 各服务商命名差异很大（QQ 用 "Sent Messages"，Gmail 用 "[Gmail]/Sent Mail"，国内还有中文名），
/// 因此按候选列表逐个探测，取第一个真实存在的。
pub fn folder_candidates(kind: FolderKind) -> &'static [&'static str] {
    match kind {
        FolderKind::Sent => &[
            "Sent",
            "Sent Messages",
            "Sent Items",
            "已发送",
            "已发送邮件",
            "[Gmail]/Sent Mail",
            "[Google Mail]/Sent Mail",
            "Sent Mail",
            "&XfJT0ZAB-", // QQ 的 UTF-7 编码形式
        ],
        FolderKind::Archive => &[
            "Archive",
            "Archives",
            "归档",
            "所有邮件",
            "[Gmail]/All Mail",
        ],
        FolderKind::Trash => &[
            "Trash",
            "Deleted Messages",
            "Deleted Items",
            "已删除",
            "已删除邮件",
            "[Gmail]/Trash",
            "&XfJSIJZk-",
        ],
        FolderKind::Drafts => &[
            "Drafts",
            "草稿箱",
            "草稿",
            "[Gmail]/Drafts",
        ],
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[allow(dead_code)] // Drafts 供后续「草稿同步到服务器」使用，当前仅在本地保存草稿
pub enum FolderKind {
    Sent,
    Archive,
    Trash,
    Drafts,
}

/// 在所有候选名里挑一个邮箱里真实存在的文件夹。
///
/// 保留为纯函数以便单测；实际探测逻辑在 `imap` 模块。
#[allow(dead_code)]
pub fn resolve_folder<F>(kind: FolderKind, mut exists: F) -> Option<String>
where
    F: FnMut(&str) -> bool,
{
    folder_candidates(kind)
        .iter()
        .find(|name| exists(name))
        .map(|s| s.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn guess_by_domain() {
        assert_eq!(guess_provider("a@qq.com"), "qq");
        assert_eq!(guess_provider("a@vip.qq.com"), "qq");
        assert_eq!(guess_provider("a@gmail.com"), "gmail");
        assert_eq!(guess_provider("A@OUTLOOK.COM"), "outlook");
        assert_eq!(guess_provider("a@company.cn"), "custom");
    }

    #[test]
    fn preset_fills_hosts() {
        let mut cfg = MailServerConfig {
            provider: "qq".into(),
            email: "me@qq.com".into(),
            imap_host: String::new(),
            imap_port: 0,
            imap_security: Security::None,
            smtp_host: String::new(),
            smtp_port: 0,
            smtp_security: Security::None,
            password: "x".into(),
        };
        apply_preset(&mut cfg).unwrap();
        assert_eq!(cfg.imap_host, "imap.qq.com");
        assert_eq!(cfg.imap_port, 993);
        assert_eq!(cfg.imap_security, Security::Ssl);
        assert_eq!(cfg.smtp_port, 465);
        assert_eq!(cfg.smtp_security, Security::Ssl);
    }

    #[test]
    fn custom_requires_hosts() {
        let mut cfg = MailServerConfig {
            provider: "custom".into(),
            email: "me@corp.cn".into(),
            imap_host: String::new(),
            imap_port: 993,
            imap_security: Security::Ssl,
            smtp_host: String::new(),
            smtp_port: 465,
            smtp_security: Security::Ssl,
            password: "x".into(),
        };
        assert!(apply_preset(&mut cfg).is_err());
        cfg.imap_host = "mail.corp.cn".into();
        cfg.smtp_host = "mail.corp.cn".into();
        assert!(apply_preset(&mut cfg).is_ok());
    }

    #[test]
    fn invalid_email_rejected() {
        let mut cfg = MailServerConfig {
            provider: "qq".into(),
            email: "not-an-email".into(),
            imap_host: String::new(),
            imap_port: 993,
            imap_security: Security::Ssl,
            smtp_host: String::new(),
            smtp_port: 465,
            smtp_security: Security::Ssl,
            password: "x".into(),
        };
        assert!(apply_preset(&mut cfg).is_err());
    }

    #[test]
    fn resolve_folder_picks_first_existing() {
        let got = resolve_folder(FolderKind::Sent, |n| n == "Sent Messages");
        assert_eq!(got.as_deref(), Some("Sent Messages"));
        assert!(resolve_folder(FolderKind::Sent, |_| false).is_none());
    }

    #[test]
    fn starttls_ports_mapped() {
        let mut cfg = MailServerConfig {
            provider: "custom".into(),
            email: "me@corp.cn".into(),
            imap_host: "mail.corp.cn".into(),
            imap_port: 143,
            imap_security: Security::None,
            smtp_host: "mail.corp.cn".into(),
            smtp_port: 587,
            smtp_security: Security::None,
            password: "x".into(),
        };
        apply_preset(&mut cfg).unwrap();
        assert_eq!(cfg.imap_security, Security::Starttls);
        assert_eq!(cfg.smtp_security, Security::Starttls);
    }
}
