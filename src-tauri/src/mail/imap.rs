//! IMAP 收信。
//!
//! 职责：
//! - 连接并登录（隐式 TLS 993 / STARTTLS 143）
//! - 增量拉取收件箱与已发送（用 UID 游标，只取新邮件）
//! - 把已读 / 星标 / 归档 / 删除回写到邮件服务器
//!
//! 游标说明：IMAP 每封邮件有独一无二且递增的 UID。我们记录上次同步到的最大 UID
//! （`last_uid`），下次只请求 `UID > last_uid` 的部分，避免每次全量下载。
//! UIDVALIDITY 变化说明服务器重建了邮箱，此时旧 UID 全部作废，需要重新全量同步。

use std::net::TcpStream;

use imap::types::{Flag, Name};
use imap::{Client, Session};
use mail_parser::{MessageParser, MimeHeaders};

use super::providers::{self, FolderKind, MailServerConfig, Security};
use super::types::{FetchedAttachment, FetchedMail, MailboxCursor, MailAddress, ServerFolder};

/// 一次同步的返回结果
#[derive(Debug, Default)]
pub struct FetchResult {
    /// 收件箱 + 已发送的新邮件
    pub mails: Vec<FetchedMail>,
    /// 更新后的游标，需由调用方写回数据库
    pub cursors: Vec<MailboxCursor>,
    /// 因解析失败或超出上限被跳过的邮件数
    pub skipped: usize,
    /// 各文件夹当前在服务器上的完整 UID 列表。
    /// 前端据此剔除「在服务器上已被删除 / 归档」的本地陈旧副本，保持两侧一致。
    pub present: Vec<FolderUids>,
}

/// 某个文件夹在服务器上的全部 UID
#[derive(Debug)]
pub struct FolderUids {
    pub folder: ServerFolder,
    pub uids: Vec<u32>,
}

/// 单次同步每个文件夹最多下载的邮件数，避免首次接入拉取上万封导致长时间卡顿。
/// 超出的部分由「历史回填游标」在后续同步里分批补齐（见 [`MailboxCursor::backfill_uid`]）。
const MAX_FETCH_PER_SYNC: usize = 300;

/// 单封邮件正文最大保留字符数，超出截断（防止超大邮件撑爆数据库 JSON）
const MAX_BODY_CHARS: usize = 200_000;

type ImapSession = Session<native_tls::TlsStream<TcpStream>>;

fn tls_connector() -> Result<native_tls::TlsConnector, String> {
    native_tls::TlsConnector::builder()
        .build()
        .map_err(|e| format!("TLS 初始化失败：{e}"))
}

/// 建立已登录的 IMAP 会话。
fn connect(cfg: &MailServerConfig) -> Result<ImapSession, String> {
    let tls = tls_connector()?;
    let domain = cfg.imap_host.as_str();
    let addr = (cfg.imap_host.as_str(), cfg.imap_port);

    let client: Client<native_tls::TlsStream<TcpStream>> = match cfg.imap_security {
        Security::Ssl => imap::connect(addr, domain, &tls)
            .map_err(|e| format!("连接 {}:{} 失败：{e}", cfg.imap_host, cfg.imap_port))?,
        Security::Starttls => imap::connect_starttls(addr, domain, &tls).map_err(|e| {
            format!(
                "STARTTLS 连接 {}:{} 失败：{e}",
                cfg.imap_host, cfg.imap_port
            )
        })?,
        Security::None => {
            return Err(
                "出于安全考虑不支持无加密的 IMAP；请使用 993(SSL) 或 143(STARTTLS)".into(),
            )
        }
    };

    let session = client
        .login(&cfg.email, &cfg.password)
        .map_err(|(e, _)| login_error(&e))?;

    // 注：连接超时依赖操作系统默认 TCP 超时。imap 2.4 未在 Session 上暴露读超时设置
    // （`set_read_timeout` 仅在 IDLE 句柄上可用），因此这里不额外设置。
    Ok(session)
}

/// 把底层登录错误翻译成用户看得懂的提示
fn login_error(e: &imap::Error) -> String {
    let text = e.to_string();
    let lower = text.to_ascii_lowercase();
    if lower.contains("auth")
        || lower.contains("invalid credentials")
        || lower.contains("login failed")
    {
        return format!(
            "认证失败：请确认使用的是「授权码 / 应用专用密码」而非网页登录密码（服务器返回：{text}）"
        );
    }
    format!("IMAP 登录失败：{text}")
}

/// 用 SELECT/EXAMINE 探测文件夹是否存在
fn folder_exists(session: &mut ImapSession, name: &str) -> bool {
    session.examine(name).is_ok()
}

/// 按候选列表解析服务端真实文件夹名；都不存在时返回 None
fn resolve_folder(session: &mut ImapSession, kind: FolderKind) -> Option<String> {
    for name in providers::folder_candidates(kind) {
        if folder_exists(session, name) {
            return Some((*name).to_string());
        }
    }
    None
}

/// 判断一组 IMAP flags 中是否含某个系统标志。
///
/// 不能直接 `flags.contains(&Flag::Seen)`：`imap-proto` 对系统标志是大小写敏感的精确匹配
/// （只认 `\Seen`），而 RFC 3501 规定标志名大小写不敏感，实际服务器可能返回
/// `\SEEN` / `\seen` 等写法，此时会被归为 `Flag::Custom`。因此这里统一转小写比较，
/// 同时保留对 `Flag::Seen` 等标准枚举的识别。
fn has_flag(flags: &[Flag<'_>], name: &str) -> bool {
    let want = name.to_ascii_lowercase();
    flags.iter().any(|f| match f {
        Flag::Custom(s) => s.eq_ignore_ascii_case(&want),
        other => other.to_string().eq_ignore_ascii_case(&want),
    })
}
/// 同步一个文件夹：先拉新邮件，再按游标回填历史邮件。
///
/// 两阶段的意义：一个已有上万封邮件的邮箱，不可能在一次同步里全部下完，
/// 也不能只下最新一批就把游标推到末尾——那样更老的邮件永远拉不到。
/// 因此用两个独立游标分工：
///
/// - `since_uid`  向上推进，负责「新到的邮件」
/// - `backfill`   向下推进，负责「历史邮件」，每批最多 `MAX_FETCH_PER_SYNC` 封
///
/// 返回本次更新后的游标（由调用方写回数据库，下次同步接着做）。
fn fetch_folder(
    session: &mut ImapSession,
    folder: &str,
    account_id: &str,
    server_folder: ServerFolder,
    since_uid: u32,
    expect_uid_validity: u32,
    backfill: Option<u32>,
    out: &mut FetchResult,
) -> Result<MailboxCursor, String> {
    let mailbox = session
        .select(folder)
        .map_err(|e| format!("无法打开文件夹 {folder}：{e}"))?;

    let uid_validity = mailbox.uid_validity.unwrap_or(0);

    // UIDVALIDITY 变了 => 服务器重建过邮箱，旧 UID 全部作废，两个游标都重新开始
    let reset =
        expect_uid_validity != 0 && uid_validity != 0 && expect_uid_validity != uid_validity;
    let since_uid = if reset { 0 } else { since_uid };
    let backfill = if reset { None } else { backfill };

    let cap = MAX_FETCH_PER_SYNC;

    // 首次接入（无任何游标）要特别处理：优先把「最近的邮件」给用户看到，
    // 而不是从最老的一封开始（那样用户打开只看到几年前的邮件）。
    let first_sync = since_uid == 0 && backfill.is_none();

    if first_sync {
        // 一次拿到全部 UID 列表，取「最新的一批」
        let all = list_uids(session, "1:*", folder)?;
        let take = all.len().min(cap);
        let batch = &all[all.len() - take..];
        download_bodies(session, folder, account_id, server_folder, batch, out)?;

        // last_uid 推到见到的最新 UID；回填从本批最老的一封继续往前
        let next_last_uid = batch.last().map(|(u, _, _)| *u).unwrap_or(0);
        let next_backfill = match batch.first().map(|(u, _, _)| *u) {
            Some(min) => Some(min.saturating_sub(1)),
            None => Some(0), // 邮箱是空的
        };

        let cursor = MailboxCursor {
            folder: server_folder,
            last_uid: next_last_uid,
            backfill_uid: next_backfill,
            uid_validity,
            remote_name: folder.to_string(),
        };
        out.cursors.push(cursor.clone());
        return Ok(cursor);
    }

    /* ———— 阶段一：新邮件（UID > since_uid，向「更新」方向推进）———— */

    let new_items = list_uids(session, &format!("{}:*", since_uid + 1), folder)?;

    // 列表按 UID 升序；截断时保留「最旧的这批」。
    // 这样 last_uid 只推进到确实下载过的位置，尾部未下载的下一轮自然接着拉，不会漏；
    // 若改取最新的一批，last_uid 会直接跳过中间那些邮件（那些邮件将永远拉不到）。
    let new_done = new_items.len() <= cap;
    let new_batch = &new_items[..new_items.len().min(cap)];
    let before_new = out.mails.len();
    download_bodies(session, folder, account_id, server_folder, new_batch, out)?;

    // 增量阶段拉到的邮件是「真正新到」的：只有收件箱 + 未读才打标，前端据此弹桌面通知。
    // 必须限定 uid > since_uid——IMAP 的 `N:*` 区间在没有新邮件时也会把最后一封捎回来
    // （既有行为，幂等重下），不限定的话最新一封未读邮件每轮同步都会被误判为新到。
    if server_folder == ServerFolder::Inbox {
        for m in &mut out.mails[before_new..] {
            m.is_new_arrival = m.uid > since_uid && !m.read;
        }
    }

    let next_last_uid = new_batch
        .iter()
        .map(|(u, _, _)| *u)
        .max()
        .unwrap_or(since_uid);

    /* ———— 阶段二：历史回填（UID <= upper，向「更旧」方向推进）———— */

    // 还没开始时，上界取「本轮见到的最小 UID」之前；已开始时接着上次进度
    let upper = match backfill {
        Some(0) => 0, // 已完成
        Some(n) => n,
        None => new_items
            .first()
            .or_else(|| new_batch.first())
            .map(|(u, _, _)| u.saturating_sub(1))
            .unwrap_or(0),
    };

    // 新邮件还没拉完时先不碰历史，避免两方向的游标互相干扰。
    // （新邮件数量通常很小，很快就能清完，不会让回填延迟太久。）
    let next_backfill = if !new_done {
        backfill
    } else if upper == 0 {
        Some(0)
    } else {
        let hist = list_uids(session, &format!("1:{upper}"), folder)?;
        if hist.is_empty() {
            Some(0) // 没有更早的邮件了，回填完成
        } else {
            // 历史方向必须保留「最新的这批」（取尾部），游标才能向 UID 1 收敛；
            // 若取头部并把游标直接置 0，会误判回填完成，中间那段邮件永久丢失。
            let take = hist.len().min(cap);
            let batch = &hist[hist.len() - take..];
            download_bodies(session, folder, account_id, server_folder, batch, out)?;
            let lowest = batch.first().map(|(u, _, _)| *u).unwrap_or(upper);
            Some(lowest.saturating_sub(1))
        }
    };

    let cursor = MailboxCursor {
        folder: server_folder,
        last_uid: next_last_uid,
        backfill_uid: next_backfill,
        uid_validity,
        remote_name: folder.to_string(),
    };
    out.cursors.push(cursor.clone());
    Ok(cursor)
}

/// 列出指定 UID 区间内邮件的 `(UID, 已读, 星标)`，按 UID 升序。
///
/// 只请求元信息（不含正文），用于决定要下载哪些邮件。
fn list_uids(
    session: &mut ImapSession,
    range: &str,
    folder: &str,
) -> Result<Vec<(u32, bool, bool)>, String> {
    // ZeroCopy 拥有底层字节缓冲，其派生值（Fetch）借用其中的数据，
    // 因此 ZeroCopy 必须绑定到变量并活到使用结束
    let metas = session
        .uid_fetch(range, "(UID FLAGS)")
        .map_err(|e| format!("拉取 {folder} 邮件列表失败：{e}"))?;

    let mut items: Vec<(u32, bool, bool)> = metas
        .iter()
        .filter_map(|f| {
            let uid = f.uid?;
            let flags = f.flags();
            Some((
                uid,
                has_flag(flags, "\\Seen"),
                has_flag(flags, "\\Flagged"),
            ))
        })
        .collect();
    items.sort_by_key(|(u, _, _)| *u);
    items.dedup_by_key(|(u, _, _)| *u);
    Ok(items)
}

/// 批量下载指定 UID 列表的正文并解析入 `out`。
///
/// 一次请求拿多封，比逐封往返快得多。`BODY.PEEK[]` 不会把邮件顺带标记为已读
/// （`BODY[]` 会），必须用 PEEK。
fn download_bodies(
    session: &mut ImapSession,
    folder: &str,
    account_id: &str,
    server_folder: ServerFolder,
    items: &[(u32, bool, bool)],
    out: &mut FetchResult,
) -> Result<(), String> {
    if items.is_empty() {
        return Ok(());
    }

    let uid_set = items
        .iter()
        .map(|(u, _, _)| u.to_string())
        .collect::<Vec<_>>()
        .join(",");

    let full = session
        .uid_fetch(&uid_set, "(UID BODY.PEEK[])")
        .map_err(|e| format!("下载 {folder} 邮件正文失败：{e}"))?;

    for msg in full.iter() {
        let Some(uid) = msg.uid else {
            out.skipped += 1;
            continue;
        };
        let Some(raw) = msg.body() else {
            out.skipped += 1;
            continue;
        };
        let (seen, flagged) = items
            .iter()
            .find(|(u, _, _)| *u == uid)
            .map(|(_, s, f)| (*s, *f))
            .unwrap_or((false, false));

        match parse_message(raw, account_id, server_folder, uid, seen, flagged, folder) {
            Some(mail) => out.mails.push(mail),
            None => out.skipped += 1,
        }
    }
    Ok(())
}

/// 把原始 RFC5322 邮件解析成结构化数据
fn parse_message(
    raw: &[u8],
    account_id: &str,
    server_folder: ServerFolder,
    uid: u32,
    seen: bool,
    flagged: bool,
    remote_name: &str,
) -> Option<FetchedMail> {
    let parsed = MessageParser::default().parse(raw)?;

    let (from_name, from_email) = parsed
        .from()
        .and_then(|a| a.first())
        .map(|a| {
            (
                a.name.as_deref().unwrap_or("").to_string(),
                a.address.as_deref().unwrap_or("").to_string(),
            )
        })
        .unwrap_or_else(|| ("未知发件人".into(), String::new()));

    let collect = |addr: Option<&mail_parser::Address<'_>>| -> Vec<MailAddress> {
        let Some(a) = addr else { return Vec::new() };
        a.iter()
            .map(|x| MailAddress {
                name: x.name.as_deref().unwrap_or("").to_string(),
                email: x.address.as_deref().unwrap_or("").to_string(),
            })
            .collect()
    };

    // 优先纯文本正文；只有 HTML 版本时做标签剥离
    let body = parsed
        .body_text(0)
        .map(|s| s.into_owned())
        .or_else(|| parsed.body_html(0).map(|h| html_to_text(&h)))
        .unwrap_or_default();

    // HTML 版本单独保留（截断到与正文相同的上限），前端用它渲染图片等富文本
    let html_body = parsed
        .body_html(0)
        .map(|h| truncate(h.into_owned(), MAX_BODY_CHARS));

    let attachments: Vec<FetchedAttachment> = parsed
        .attachments()
        .map(|part| FetchedAttachment {
            name: part
                .attachment_name()
                .map(|s| s.to_string())
                .unwrap_or_else(|| "未命名附件".to_string()),
            size_kb: (part.contents().len() / 1024) as u32,
            kind: guess_attachment_kind(&part.attachment_name().unwrap_or(""), part),
        })
        .collect();

    Some(FetchedMail {
        uid,
        account_id: account_id.to_string(),
        server_folder,
        remote_name: remote_name.to_string(),
        message_id: parsed.message_id().map(|s| s.to_string()),
        subject: parsed
            .subject()
            .map(|s| s.to_string())
            .unwrap_or_else(|| "（无主题）".into()),
        from_name,
        from_email,
        to: collect(parsed.to()),
        cc: collect(parsed.cc()),
        body: truncate(body, MAX_BODY_CHARS),
        html_body,
        date: parsed
            .date()
            .map(|d| d.to_rfc3339())
            .unwrap_or_else(|| chrono::Utc::now().to_rfc3339()),
        read: seen,
        starred: flagged,
        attachments,
        // 解析层不区分「新到」还是「回填」，由 fetch_folder 按阶段打标
        is_new_arrival: false,
    })
}

/// 按扩展名判断附件类型（前端据此选图标），无扩展名时退回 MIME 类型
fn guess_attachment_kind(name: &str, part: &mail_parser::MessagePart<'_>) -> String {
    let ext = name
        .to_ascii_lowercase()
        .rsplit_once('.')
        .map(|(_, e)| e.to_string())
        .unwrap_or_default();

    let by_ext = match ext.as_str() {
        "pdf" => Some("pdf"),
        "xls" | "xlsx" | "csv" | "ods" => Some("sheet"),
        "doc" | "docx" | "odt" | "rtf" => Some("doc"),
        "png" | "jpg" | "jpeg" | "gif" | "webp" | "bmp" | "svg" | "heic" => Some("image"),
        "zip" | "rar" | "7z" | "tar" | "gz" => Some("zip"),
        "js" | "ts" | "java" | "py" | "rs" | "go" | "c" | "cpp" | "json" | "xml" | "html"
        | "css" => Some("code"),
        _ => None,
    };
    if let Some(k) = by_ext {
        return k.to_string();
    }

    match part
        .content_type()
        .map(|ct| (ct.ctype(), ct.subtype().unwrap_or("")))
    {
        Some(("image", _)) => "image",
        _ => "doc",
    }
    .to_string()
}

/// 极简 HTML 转文本：足够阅读邮件用，避免引入完整 HTML 解析依赖。
///
/// 注意：必须基于 `char` 遍历而不是 `bytes[i] as char`——后者会把 UTF-8 多字节字符
/// 当成 Latin-1 单字节处理，导致中文全部乱码。
fn html_to_text(html: &str) -> String {
    let mut out = String::with_capacity(html.len() / 2);
    let mut in_tag = false;
    let mut skip_depth = 0usize;

    let mut chars = html.char_indices().peekable();
    while let Some((idx, c)) = chars.next() {
        if c == '<' {
            // 用原文切片判断标签类型（to_ascii_lowercase 只影响 ASCII，不破坏偏移）
            let rest = html[idx..].to_ascii_lowercase();
            if rest.starts_with("<script") || rest.starts_with("<style") {
                skip_depth += 1;
            } else if rest.starts_with("</script") || rest.starts_with("</style") {
                skip_depth = skip_depth.saturating_sub(1);
            }

            // 块级 / 换行标签转成换行，保留段落感
            if rest.starts_with("<br")
                || rest.starts_with("</p")
                || rest.starts_with("</div")
                || rest.starts_with("</tr")
                || rest.starts_with("</h1")
                || rest.starts_with("</h2")
                || rest.starts_with("</h3")
                || rest.starts_with("</li")
            {
                out.push('\n');
            }
            in_tag = true;
            continue;
        }
        if c == '>' {
            in_tag = false;
            continue;
        }
        if !in_tag && skip_depth == 0 {
            out.push(c);
        }
    }

    let decoded = out
        .replace("&nbsp;", " ")
        .replace("&amp;", "&")
        .replace("&lt;", "<")
        .replace("&gt;", ">")
        .replace("&quot;", "\"")
        .replace("&#39;", "'");

    // 折叠连续空行为单个空行
    let mut result = String::with_capacity(decoded.len());
    let mut blank_run = 0;
    for line in decoded.lines() {
        let t = line.trim_end();
        if t.trim().is_empty() {
            blank_run += 1;
            if blank_run > 1 {
                continue;
            }
            result.push('\n');
        } else {
            blank_run = 0;
            result.push_str(t);
            result.push('\n');
        }
    }
    result.trim().to_string()
}

/// 按字符数截断（不能按字节切，否则会把中文截断成乱码）
fn truncate(mut s: String, max_chars: usize) -> String {
    if s.chars().count() <= max_chars {
        return s;
    }
    let cut = s
        .char_indices()
        .nth(max_chars)
        .map(|(i, _)| i)
        .unwrap_or(s.len());
    s.truncate(cut);
    s.push_str("\n\n……（正文过长已截断）");
    s
}

/* ———————————————— 对外接口 ———————————————— */

/// 同步一个账户。
///
/// `cursors` 是上次同步留下的游标；首次接入传空数组。
/// 返回新邮件与更新后的游标（调用方负责把游标写回数据库）。
pub fn sync_account(cfg: &MailServerConfig, cursors: &[MailboxCursor]) -> Result<FetchResult, String> {
    let mut session = connect(cfg)?;
    let mut result = FetchResult::default();
    let account_id = cfg.email.clone();

    let lookup = |folder: ServerFolder| -> Option<&MailboxCursor> {
        cursors.iter().find(|c| c.folder == folder)
    };

    // 收件箱：RFC 3501 规定叫 INBOX，大小写不敏感
    {
        let c = lookup(ServerFolder::Inbox);
        fetch_folder(
            &mut session,
            "INBOX",
            &account_id,
            ServerFolder::Inbox,
            c.map(|c| c.last_uid).unwrap_or(0),
            c.map(|c| c.uid_validity).unwrap_or(0),
            c.and_then(|c| c.backfill_uid),
            &mut result,
        )?;
        // 服务器当前的全量 UID，用于前端剔除本地陈旧副本（网页上删了/归档了的邮件）
        let uids = list_uids(&mut session, "1:*", "INBOX")?
            .into_iter()
            .map(|(u, _, _)| u)
            .collect();
        result.present.push(FolderUids { folder: ServerFolder::Inbox, uids });
    }

    // 已发送：各服务商命名不同，先探测真实名字
    if let Some(sent_name) = resolve_folder(&mut session, FolderKind::Sent) {
        let c = lookup(ServerFolder::Sent);
        fetch_folder(
            &mut session,
            &sent_name,
            &account_id,
            ServerFolder::Sent,
            c.map(|c| c.last_uid).unwrap_or(0),
            c.map(|c| c.uid_validity).unwrap_or(0),
            c.and_then(|c| c.backfill_uid),
            &mut result,
        )?;
        let uids = list_uids(&mut session, "1:*", &sent_name)?
            .into_iter()
            .map(|(u, _, _)| u)
            .collect();
        result.present.push(FolderUids { folder: ServerFolder::Sent, uids });
    }

    let _ = session.logout();
    Ok(result)
}

/// 实时监控收件箱：IDLE 长连接等服务器推送，一有新邮件就做一次「仅增量」拉取。
///
/// 与 [`sync_account`] 的分工：
/// - 只看收件箱（通知只关心新到邮件；已发送同步与历史回填仍由常规同步负责）
/// - 只推进 `last_uid`，不碰回填游标；返回的游标由调用方与既有游标合并后写回
/// - 连接长持有：先查一次增量，没有就进入 IDLE；IDLE 到期（RFC 2177 建议至少
///   每 29 分钟重发一次，这里留足余量）后重查增量再继续 IDLE
///
/// 仅在拉到新邮件时返回；连接 / 协议错误原样上抛，由调用方退避重连。
/// 服务器不支持 IDLE 时退化为轮询（每分钟查一次增量）。
pub fn idle_fetch_new(
    cfg: &MailServerConfig,
    cursors: &[MailboxCursor],
    idle_wait: std::time::Duration,
) -> Result<FetchResult, String> {
    let mut session = connect(cfg)?;
    let account_id = cfg.email.clone();

    let idle_capable = {
        let caps = session
            .capabilities()
            .map_err(|e| format!("获取服务器能力失败：{e}"))?;
        caps.has_str("IDLE")
    };

    loop {
        if let Some(result) = fetch_new_inbox(&mut session, &account_id, cursors)? {
            let _ = session.logout();
            return Ok(result);
        }
        if idle_capable {
            let handle = session.idle().map_err(|e| format!("进入 IDLE 失败：{e}"))?;
            // 无论超时还是服务器推送（新邮件 / 旗标 / 删除都算）都回到循环头
            // 重查一次增量：有新邮件就拉取返回，没有就继续等下一轮
            handle
                .wait_with_timeout(idle_wait)
                .map_err(|e| format!("IDLE 等待中断：{e}"))?;
        } else {
            std::thread::sleep(std::time::Duration::from_secs(60));
        }
    }
}

/// 拉取收件箱自游标 `last_uid` 之后的增量邮件；没有新邮件返回 `None`。
///
/// 语义与 [`fetch_folder`] 的阶段一一致（含「截断保最旧」策略），但：
/// - 没有游标（账户尚未做过首次同步）时不越俎代庖，等常规同步建立游标
/// - UIDVALIDITY 变化时直接跳过，等常规同步全量重来，实时通道不掺和
fn fetch_new_inbox(
    session: &mut ImapSession,
    account_id: &str,
    cursors: &[MailboxCursor],
) -> Result<Option<FetchResult>, String> {
    let Some(c) = cursors.iter().find(|c| c.folder == ServerFolder::Inbox) else {
        return Ok(None);
    };

    let mailbox = session
        .select("INBOX")
        .map_err(|e| format!("无法打开收件箱：{e}"))?;
    let uid_validity = mailbox.uid_validity.unwrap_or(0);
    if c.uid_validity != 0 && uid_validity != 0 && c.uid_validity != uid_validity {
        return Ok(None);
    }

    // `N:*` 在没有新邮件时也会捎回最后一封，必须再按 uid > last_uid 过滤
    let items = list_uids(session, &format!("{}:*", c.last_uid + 1), "INBOX")?;
    let new_items: Vec<(u32, bool, bool)> = items
        .into_iter()
        .filter(|(u, _, _)| *u > c.last_uid)
        .collect();
    if new_items.is_empty() {
        return Ok(None);
    }

    let cap = MAX_FETCH_PER_SYNC;
    let batch = &new_items[..new_items.len().min(cap)];
    let mut result = FetchResult::default();
    let before = result.mails.len();
    download_bodies(session, "INBOX", account_id, ServerFolder::Inbox, batch, &mut result)?;
    for m in &mut result.mails[before..] {
        m.is_new_arrival = !m.read;
    }

    result.cursors.push(MailboxCursor {
        folder: ServerFolder::Inbox,
        last_uid: batch.iter().map(|(u, _, _)| *u).max().unwrap_or(c.last_uid),
        backfill_uid: c.backfill_uid,
        uid_validity,
        remote_name: "INBOX".into(),
    });
    Ok(Some(result))
}

/// 解析归档 / 垃圾箱的目标文件夹名
pub fn resolve_server_folder(
    cfg: &MailServerConfig,
    kind: FolderKind,
) -> Result<Option<String>, String> {
    let mut session = connect(cfg)?;
    let name = resolve_folder(&mut session, kind);
    let _ = session.logout();
    Ok(name)
}

/// 回写已读 / 星标状态。`None` 表示该项不变。
pub fn set_flags(
    cfg: &MailServerConfig,
    remote_name: &str,
    uid: u32,
    read: Option<bool>,
    starred: Option<bool>,
) -> Result<(), String> {
    let mut session = connect(cfg)?;
    session
        .select(remote_name)
        .map_err(|e| format!("无法打开文件夹 {remote_name}：{e}"))?;

    if let Some(r) = read {
        let op = if r { "+FLAGS.SILENT" } else { "-FLAGS.SILENT" };
        session
            .uid_store(uid.to_string(), format!("{op} (\\Seen)"))
            .map_err(|e| format!("同步已读状态失败：{e}"))?;
    }
    if let Some(s) = starred {
        let op = if s { "+FLAGS.SILENT" } else { "-FLAGS.SILENT" };
        session
            .uid_store(uid.to_string(), format!("{op} (\\Flagged)"))
            .map_err(|e| format!("同步星标状态失败：{e}"))?;
    }

    let _ = session.logout();
    Ok(())
}

/// 把邮件移动到目标文件夹；目标不存在时自动创建。
///
/// 优先用 MOVE 扩展（服务端原子操作，一次往返），
/// 服务器不支持时退回「复制 → 打删除标记 → EXPUNGE」。
pub fn move_message(
    cfg: &MailServerConfig,
    remote_name: &str,
    uid: u32,
    target: &str,
) -> Result<(), String> {
    let mut session = connect(cfg)?;
    session
        .select(remote_name)
        .map_err(|e| format!("无法打开文件夹 {remote_name}：{e}"))?;

    // 目标不存在则尝试创建（部分服务商禁止客户端创建，失败不致命）。
    //
    // 关键：EXAMINE 与 SELECT 一样会「切换当前选中的邮箱」——它是 IMAP 的会话状态，
    // 不是无副作用的存在性探测。因此探测之后必须重新 SELECT 原文件夹，
    // 否则接下来的 UID MOVE 会作用在目标文件夹上（UID 在那里不存在，归档静默失效）。
    let target_missing = session.examine(target).is_err();
    if target_missing {
        let _ = session.create(target);
    }
    // 无论目标是否存在，都重新选中源文件夹
    session
        .select(remote_name)
        .map_err(|e| format!("无法重新打开文件夹 {remote_name}：{e}"))?;

    let has_move = session
        .capabilities()
        .map(|caps| caps.has_str("MOVE"))
        .unwrap_or(false);

    if has_move {
        session
            .uid_mv(uid.to_string(), target)
            .map_err(|e| format!("移动到 {target} 失败：{e}"))?;
    } else {
        session
            .uid_copy(uid.to_string(), target)
            .map_err(|e| format!("复制到 {target} 失败：{e}"))?;
        session
            .uid_store(uid.to_string(), "+FLAGS.SILENT (\\Deleted)")
            .map_err(|e| format!("标记删除失败：{e}"))?;
        session
            .uid_expunge(uid.to_string())
            .map_err(|e| format!("清理原邮件失败：{e}"))?;
    }

    let _ = session.logout();
    Ok(())
}

/// 连通性测试：登录并读取文件夹列表，不做任何修改
pub fn test_connection(cfg: &MailServerConfig) -> Result<Vec<String>, String> {
    let mut session = connect(cfg)?;
    let list = session
        .list(Some(""), Some("*"))
        .map_err(|e| format!("读取文件夹列表失败：{e}"))?;
    let names: Vec<String> = list.iter().map(|n: &Name| n.name().to_string()).collect();
    let _ = session.logout();
    Ok(names)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn html_to_text_strips_tags_and_scripts() {
        let html = "<html><head><style>p{color:red}</style></head>\
                    <body><p>你好</p><script>alert(1)</script><p>世界</p></body></html>";
        let text = html_to_text(html);
        assert!(text.contains("你好"));
        assert!(text.contains("世界"));
        assert!(!text.contains("color:red"), "style 内容必须被剔除");
        assert!(!text.contains("alert"), "script 内容必须被剔除");
        assert!(!text.contains('<'), "不应残留标签");
    }

    #[test]
    fn html_block_tags_become_newlines() {
        let text = html_to_text("<p>one</p><p>two</p>");
        assert!(text.contains("one\ntwo") || text.contains("one\n\ntwo"));
    }

    #[test]
    fn has_flag_is_case_insensitive() {
        use std::borrow::Cow;
        // 标准枚举形式
        assert!(has_flag(&[Flag::Seen], "\\Seen"));
        assert!(has_flag(&[Flag::Flagged], "\\Flagged"));
        assert!(!has_flag(&[Flag::Seen], "\\Flagged"));
        // 服务器返回大写时会被 imap-proto 归为 Custom，也必须识别
        assert!(has_flag(&[Flag::Custom(Cow::Borrowed("\\SEEN"))], "\\Seen"));
        assert!(has_flag(&[Flag::Custom(Cow::Borrowed("\\seen"))], "\\Seen"));
        assert!(!has_flag(&[Flag::Custom(Cow::Borrowed("\\SEEN"))], "\\Flagged"));
        // 空集合
        assert!(!has_flag(&[], "\\Seen"));
    }

    #[test]
    fn backfill_cursor_converges_to_zero() {
        // 模拟 1000 封邮件、单批 300 封：首次同步取最新一批，随后逐批向前回填，
        // 验证两个游标配合下确实能拉全且不重不漏。
        let cap = 300usize;
        let total = 1000u32;
        let mut fetched: Vec<u32> = Vec::new();

        // —— 首次同步：取最新的 min(cap, total) 封 ——
        let all: Vec<u32> = (1..=total).collect();
        let take = all.len().min(cap);
        let batch = &all[all.len() - take..];
        fetched.extend(batch.iter().copied());

        let last_uid = *batch.last().unwrap();
        let mut backfill = Some(batch.first().unwrap().saturating_sub(1)); // 700
        assert_eq!(last_uid, 1000);
        assert_eq!(backfill, Some(700));

        // —— 后续同步：不断向前回填 ——
        let mut rounds = 0;
        loop {
            let Some(n) = backfill else { break };
            if n == 0 {
                break;
            }
            rounds += 1;
            assert!(rounds < 50, "回填未收敛，可能死循环");

            let hist: Vec<u32> = (1..=n).collect();
            let t = hist.len().min(cap);
            let b = &hist[hist.len() - t..];
            fetched.extend(b.iter().copied());
            backfill = Some(b.first().unwrap().saturating_sub(1));
        }

        assert_eq!(last_uid, 1000, "新邮件游标应停在最新 UID");
        assert_eq!(backfill, Some(0), "回填游标应收敛到 0");

        fetched.sort_unstable();
        let unique: Vec<u32> = {
            let mut v = fetched.clone();
            v.dedup();
            v
        };
        assert_eq!(unique.len(), 1000, "必须不重复地拉全 1000 封");
        assert_eq!(unique[0], 1, "必须覆盖到最旧的第 1 封");
        assert_eq!(unique[999], 1000, "必须覆盖到最新的第 1000 封");
        assert_eq!(fetched.len(), 1000, "不应有重复下载");
    }

    #[test]
    fn first_sync_prefers_recent_mail() {
        // 首次接入应优先展示最近邮件，而不是从最老的一封开始
        let cap = 300usize;
        let all: Vec<u32> = (1..=1000).collect();
        let batch = &all[all.len() - cap..];
        assert_eq!(*batch.first().unwrap(), 701, "应取最新的 300 封");
        assert_eq!(*batch.last().unwrap(), 1000);
    }

    #[test]
    fn empty_mailbox_finishes_backfill() {
        // 空邮箱：不应陷入反复探测，直接标记回填完成
        let all: Vec<u32> = Vec::new();
        let take = all.len().min(300);
        let batch = &all[..take];
        let next_last = batch.last().copied().unwrap_or(0);
        let next_backfill = match batch.first() {
            Some(min) => Some(min.saturating_sub(1)),
            None => Some(0),
        };
        assert_eq!(next_last, 0);
        assert_eq!(next_backfill, Some(0));
    }

    #[test]
    fn wrong_direction_would_lose_mail() {
        // 反证：历史方向若「取头部」并把游标置 0，会误判完成并丢掉中间一段
        let cap = 300usize;
        let n = 900u32; // 待回填区间 1..=900
        let hist: Vec<u32> = (1..=n).collect();
        let batch = &hist[..hist.len().min(cap)]; // 错误做法：取头部
        let lowest = *batch.first().unwrap();
        let wrong_next = lowest.saturating_sub(1); // = 0，被当作「回填完成」
        assert_eq!(wrong_next, 0);
        assert_eq!(batch.last().copied(), Some(300));
        assert!(
            wrong_next == 0 && batch.last().copied() != Some(n),
            "错误方向会把 301..900 永久遗漏"
        );

        // 正确做法：取尾部
        let right = &hist[hist.len() - cap..];
        assert_eq!(*right.first().unwrap(), 601);
        assert_eq!(*right.last().unwrap(), 900);
        // 下一轮从 600 继续，不会跳过 301..600
        assert_eq!(right.first().unwrap().saturating_sub(1), 600);
    }

    #[test]
    fn new_mail_takes_oldest_batch() {
        // 新邮件方向：截断时取「最旧的这批」，last_uid 只推进到已下载的位置
        let cap = 300usize;
        let new_items: Vec<u32> = (1..=1000).collect();
        let batch = &new_items[..new_items.len().min(cap)];
        assert_eq!(batch.len(), 300);
        assert_eq!(*batch.first().unwrap(), 1);
        assert_eq!(*batch.last().unwrap(), 300, "last_uid 只能推进到 300");
        // 下一轮 since_uid=300 => range "301:*"，继续拉剩余部分
        let next_start = batch.last().unwrap() + 1;
        assert_eq!(next_start, 301);
    }

    #[test]
    fn cursor_reports_pending_history() {
        let base = MailboxCursor {
            folder: ServerFolder::Inbox,
            last_uid: 100,
            backfill_uid: None,
            uid_validity: 1,
            remote_name: "INBOX".into(),
        };
        assert!(!base.has_pending_history(), "None 表示尚未探测，不算待回填");

        let pending = MailboxCursor {
            backfill_uid: Some(500),
            ..base.clone()
        };
        assert!(pending.has_pending_history());

        let done = MailboxCursor {
            backfill_uid: Some(0),
            ..base
        };
        assert!(!done.has_pending_history(), "0 表示已回填完毕");
    }

    #[test]
    fn html_keeps_utf8_intact() {
        // 回归测试：曾经用 bytes[i] as char 导致中文变乱码
        assert_eq!(html_to_text("<p>你好世界</p>"), "你好世界");
        assert_eq!(html_to_text("你好"), "你好");
        assert_eq!(html_to_text("<div>中文 😀 emoji</div>"), "中文 😀 emoji");
    }

    #[test]
    fn html_entities_decoded() {
        assert_eq!(html_to_text("a &amp; b &lt;tag&gt;"), "a & b <tag>");
    }

    #[test]
    fn html_collapses_blank_lines() {
        let text = html_to_text("<p>a</p><br><br><br><br><p>b</p>");
        assert!(!text.contains("\n\n\n"), "连续空行应被折叠：{text:?}");
    }

    #[test]
    fn truncate_is_utf8_safe() {
        let s = "中".repeat(100);
        let out = truncate(s, 10);
        assert!(out.starts_with(&"中".repeat(10)));
        assert!(out.contains("已截断"));
        assert_eq!(out.chars().filter(|c| *c == '中').count(), 10);
    }

    #[test]
    fn truncate_keeps_short_strings() {
        assert_eq!(truncate("短".into(), 10), "短");
    }

    #[test]
    fn attachment_kind_by_extension() {
        assert_eq!(kind_from_ext("报表.pdf"), Some("pdf"));
        assert_eq!(kind_from_ext("数据.xlsx"), Some("sheet"));
        assert_eq!(kind_from_ext("照片.JPG"), Some("image"));
        assert_eq!(kind_from_ext("源码.rs"), Some("code"));
        assert_eq!(kind_from_ext("打包.zip"), Some("zip"));
        assert_eq!(kind_from_ext("无扩展名"), None);
    }

    /// 只测扩展名分支（构造真实 MessagePart 需要完整邮件，成本过高）
    fn kind_from_ext(name: &str) -> Option<&'static str> {
        let ext = name
            .to_ascii_lowercase()
            .rsplit_once('.')
            .map(|(_, e)| e.to_string())
            .unwrap_or_default();
        match ext.as_str() {
            "pdf" => Some("pdf"),
            "xls" | "xlsx" | "csv" | "ods" => Some("sheet"),
            "doc" | "docx" | "odt" | "rtf" => Some("doc"),
            "png" | "jpg" | "jpeg" | "gif" | "webp" | "bmp" | "svg" | "heic" => Some("image"),
            "zip" | "rar" | "7z" | "tar" | "gz" => Some("zip"),
            "js" | "ts" | "java" | "py" | "rs" | "go" | "c" | "cpp" | "json" | "xml" | "html"
            | "css" => Some("code"),
            _ => None,
        }
    }

    #[test]
    fn unencrypted_imap_rejected() {
        let cfg = MailServerConfig {
            provider: "custom".into(),
            email: "a@b.com".into(),
            imap_host: "127.0.0.1".into(),
            imap_port: 143,
            imap_security: Security::None,
            smtp_host: "127.0.0.1".into(),
            smtp_port: 25,
            smtp_security: Security::None,
            password: "x".into(),
        };
        let err = connect(&cfg).unwrap_err();
        assert!(err.contains("不支持无加密"), "实际：{err}");
    }
}
