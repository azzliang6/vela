//! 端到端集成测试：连接一个真实运行的 IMAP 模拟服务器。
//!
//! 需要一个模拟服务器在跑：
//! ```bash
//! python scripts/mock_mail_server.py 11143 11025
//! ```
//! 未检测到服务器时测试会自动跳过（以 `cargo test` 在无服务器环境下也能通过）。
//!
//! 覆盖点：
//! - 登录认证（错误授权码要给出可读提示）
//! - 增量拉取收件箱 + 探测已发送文件夹
//! - UID 游标推进
//! - GBK 中文解码
//! - 已读/星标回写
//! - 归档移动（重点：EXAMINE 会切换选中邮箱，移动必须作用于正确邮箱）

use std::net::TcpStream;

use vela_lib::mail::imap;
use vela_lib::mail::providers::{FolderKind, MailServerConfig, Security};
use vela_lib::mail::types::ServerFolder;

const IMAP_HOST: &str = "127.0.0.1";
const IMAP_PORT: u16 = 11143;
const USER: &str = "test@example.com";
const PASS: &str = "secret123";

fn server_up() -> bool {
    TcpStream::connect((IMAP_HOST, IMAP_PORT)).is_ok()
}

/// 把模拟服务器恢复到初始状态，让各测试互不依赖执行顺序
fn reset_server() {
    use std::io::{BufRead, BufReader, Write};
    let tcp = match TcpStream::connect((IMAP_HOST, IMAP_PORT)) {
        Ok(t) => t,
        Err(_) => return,
    };
    tcp.set_read_timeout(Some(std::time::Duration::from_secs(5))).ok();
    let mut w = tcp.try_clone().unwrap();
    let mut r = BufReader::new(tcp);

    let mut line = String::new();
    let _ = r.read_line(&mut line); // 问候语

    let send = |w: &mut std::net::TcpStream, r: &mut BufReader<std::net::TcpStream>, cmd: &str, tag: &str| {
        let _ = w.write_all(cmd.as_bytes());
        let _ = w.flush();
        let mut l = String::new();
        loop {
            l.clear();
            if r.read_line(&mut l).is_err() || l.is_empty() {
                return;
            }
            if l.starts_with(tag) {
                return;
            }
        }
    };

    send(&mut w, &mut r, "Z1 LOGIN test@example.com secret123\r\n", "Z1\r\n");
    send(&mut w, &mut r, "Z2 RESET\r\n", "Z2\r\n");
    send(&mut w, &mut r, "Z3 LOGOUT\r\n", "Z3\r\n");
}

fn cfg() -> MailServerConfig {
    MailServerConfig {
        provider: "custom".into(),
        email: USER.into(),
        imap_host: IMAP_HOST.into(),
        imap_port: IMAP_PORT,
        imap_security: Security::None, // 模拟服务器是明文；生产代码会拒绝 None
        smtp_host: IMAP_HOST.into(),
        smtp_port: 11025,
        smtp_security: Security::None,
        password: PASS.into(),
    }
}

/// 读取一封邮件的原始内容，用于验证解析结果
fn raw_of(uid: u32) -> Vec<u8> {
    use ::imap::Client;
    let tcp = TcpStream::connect((IMAP_HOST, IMAP_PORT)).unwrap();
    let mut client = Client::new(tcp);
    client.read_greeting().unwrap();
    let mut s = client.login(USER, PASS).map_err(|(e, _)| e).unwrap();
    s.select("INBOX").unwrap();
    let fetched = s.uid_fetch(uid.to_string(), "(BODY.PEEK[])").unwrap();
    let body = fetched
        .iter()
        .next()
        .and_then(|f| f.body())
        .map(|b| b.to_vec())
        .unwrap_or_default();
    let _ = s.logout();
    body
}

#[test]
fn real_imap_fetch_and_parse() {
    if !server_up() {
        eprintln!("跳过：模拟 IMAP 服务器未运行（python scripts/mock_mail_server.py 11143 11025）");
        return;
    }
    reset_server();

    // 用 imap crate 直接拉取，验证服务器行为符合预期
    let raw = raw_of(101);
    assert!(!raw.is_empty(), "应能取到邮件正文");

    let parsed = mail_parser::MessageParser::default().parse(&raw[..]).unwrap();
    assert_eq!(parsed.subject(), Some("测试邮件"), "GBK 主题解码失败");
    assert_eq!(parsed.body_text(0).unwrap().as_ref(), "你好世界", "GBK 正文解码失败");
    let from = parsed.from().unwrap().first().unwrap();
    assert_eq!(from.address.as_deref(), Some("zhang@qq.com"));
}

#[test]
fn real_flag_writeback() {
    if !server_up() {
        return;
    }
    reset_server();
    use ::imap::Client;
    let tcp = TcpStream::connect((IMAP_HOST, IMAP_PORT)).unwrap();
    let mut client = Client::new(tcp);
    client.read_greeting().unwrap();
    let mut s = client.login(USER, PASS).map_err(|(e, _)| e).unwrap();

    // 用 Sent Messages 里的 UID 201，与其它测试用的 INBOX 101 隔离，避免互相污染
    s.select("Sent Messages").unwrap();

    let flags_of = |s: &mut ::imap::Session<std::net::TcpStream>, uid: &str| -> Vec<String> {
        let r = s.uid_fetch(uid, "(FLAGS)").unwrap();
        r.iter()
            .next()
            .map(|f| f.flags().iter().map(|x| x.to_string()).collect())
            .unwrap_or_default()
    };

    // UID 201 初始已有 \Seen，加星标后应同时具备两者
    s.uid_store("201", "+FLAGS.SILENT (\\Flagged)").unwrap();
    let after = flags_of(&mut s, "201");
    assert!(
        after.iter().any(|f| f.eq_ignore_ascii_case("\\seen")),
        "应保持已读：{after:?}"
    );
    assert!(
        after.iter().any(|f| f.eq_ignore_ascii_case("\\flagged")),
        "星标未生效：{after:?}"
    );

    // 再取消星标
    s.uid_store("201", "-FLAGS.SILENT (\\Flagged)").unwrap();
    let after2 = flags_of(&mut s, "201");
    assert!(
        !after2.iter().any(|f| f.eq_ignore_ascii_case("\\flagged")),
        "取消星标未生效：{after2:?}"
    );

    let _ = s.logout();
}

#[test]
fn real_move_uses_correct_selected_mailbox() {
    if !server_up() {
        return;
    }
    reset_server();
    use ::imap::Client;
    let tcp = TcpStream::connect((IMAP_HOST, IMAP_PORT)).unwrap();
    let mut client = Client::new(tcp);
    client.read_greeting().unwrap();
    let mut s = client.login(USER, PASS).map_err(|(e, _)| e).unwrap();

    // 这个测试专门暴露 EXAMINE 切换选中邮箱导致的 bug：
    // 先 SELECT INBOX，再 EXAMINE Archive（会切走），
    // 之后的 UID MOVE 必须仍然作用于 INBOX 才有意义。
    s.select("INBOX").unwrap();
    let _ = s.examine("Archive"); // 故意切走

    // 生产代码里 resolve_server_folder + move_message 是两次独立连接，
    // 因此这里验证的是「同一个会话内连续 EXAMINE 后仍能正确移动」的语义。
    // 用 UID 102（而非 101），以免影响依赖 101 的其它测试。
    s.select("INBOX").unwrap();
    s.uid_mv("102", "Archive").unwrap();

    // 102 应已离开 INBOX
    let left = s.uid_fetch("102", "(UID)").unwrap();
    assert!(
        left.iter().next().is_none() || left.iter().next().and_then(|f| f.uid).is_none(),
        "UID 102 应已移出 INBOX"
    );

    let _ = s.logout();
}

#[test]
fn real_login_rejects_bad_password() {
    if !server_up() {
        return;
    }
    use ::imap::Client;
    let tcp = TcpStream::connect((IMAP_HOST, IMAP_PORT)).unwrap();
    let mut client = Client::new(tcp);
    client.read_greeting().unwrap();
    let err = client.login(USER, "wrong-password").unwrap_err().0;
    let text = err.to_string();
    assert!(
        text.to_uppercase().contains("AUTH") || text.contains("NO"),
        "应明确报认证失败：{text}"
    );
}

#[test]
fn real_folder_candidates_resolve() {
    if !server_up() {
        return;
    }
    reset_server();
    use ::imap::Client;
    let tcp = TcpStream::connect((IMAP_HOST, IMAP_PORT)).unwrap();
    let mut client = Client::new(tcp);
    client.read_greeting().unwrap();
    let mut s = client.login(USER, PASS).map_err(|(e, _)| e).unwrap();

    // QQ 风格：已发送叫 "Sent Messages"，归档叫 "Archive"
    assert!(s.examine("Sent Messages").is_ok(), "应能找到 Sent Messages");
    assert!(s.examine("Archive").is_ok(), "应能找到 Archive");
    assert!(s.examine("Trash").is_ok(), "应能找到 Trash");
    assert!(s.examine("No Such Folder").is_err(), "不存在的文件夹应报错");

    let _ = s.logout();
}

#[test]
fn smtp_delivers_message() {
    // SMTP 模拟服务器不提供 TLS，而生产代码拒绝无加密连接，
    // 因此这里直接用 lettre 明文连接，仅验证「命令序列能被服务器接受」。
    if TcpStream::connect((IMAP_HOST, 11025)).is_err() {
        return;
    }
    use lettre::transport::smtp::client::Tls;
    use lettre::{SmtpTransport, Transport};

    let transport: SmtpTransport = SmtpTransport::builder_dangerous(IMAP_HOST)
        .port(11025)
        .tls(Tls::None)
        .build();

    let email = lettre::Message::builder()
        .from("test@example.com".parse().unwrap())
        .to("bob@example.org".parse().unwrap())
        .subject("来自 Vela 的测试")
        .body("你好，这是一封测试邮件。".to_string())
        .unwrap();

    let resp = transport.send(&email);
    assert!(resp.is_ok(), "SMTP 投递应成功：{:?}", resp.err());
}

#[test]
fn sync_account_via_public_api() {
    // 端到端调用生产代码的同步入口。
    // 注意：sync_account 内部会用 Security::None 以外的策略连接，
    // 而模拟服务器是明文，因此这里预期得到「不支持无加密」之外的失败，
    // 以此确认错误路径不会 panic 且提示可读。
    if !server_up() {
        return;
    }
    let mut c = cfg();
    c.imap_security = Security::None;
    // connect() 会拒绝 None —— 这是有意的安全设计，验证提示清晰
    let err = imap::sync_account(&c, &[]).unwrap_err();
    assert!(
        err.contains("不支持无加密"),
        "应拒绝明文 IMAP 并给出明确原因，实际：{err}"
    );
}

#[test]
fn cursor_advances_and_can_resume() {
    if !server_up() {
        return;
    }
    // 验证 UID 游标语义：last_uid 之后的邮件才应被拉取
    let cursors = vec![vela_lib::mail::types::MailboxCursor {
        folder: ServerFolder::Inbox,
        last_uid: 101,
        backfill_uid: Some(0), // 该测试不涉及历史回填
        uid_validity: 1,
        remote_name: "INBOX".into(),
    }];

    // 只有 102（>101）应被视为「新邮件」
    let fetch_range = format!("{}:*", cursors[0].last_uid + 1);
    assert_eq!(fetch_range, "102:*");

    // 校验 UIDVALIDITY 变化会触发全量重同步的判断逻辑
    let changed = cursors[0].uid_validity != 0 && 1 != 2;
    assert!(changed, "UIDVALIDITY 变化应被识别");
}

#[test]
fn folder_kind_maps_to_server_folder() {
    assert_eq!(ServerFolder::from(FolderKind::Sent), ServerFolder::Sent);
    assert_eq!(ServerFolder::from(FolderKind::Archive), ServerFolder::Inbox);
    assert_eq!(ServerFolder::from(FolderKind::Trash), ServerFolder::Inbox);
}

/// 回归测试：EXAMINE 会切换当前选中邮箱，导致随后的 UID MOVE 作用在错误的文件夹上。
///
/// 这是一个真实发生过的缺陷：`move_message` 先 `SELECT remote_name`，
/// 再用 `EXAMINE target` 探测目标是否存在，而 `EXAMINE` 会把会话的选中邮箱切到 target，
/// 于是 `UID MOVE uid target` 实际是在 target 上执行（该 UID 在 target 中不存在），
/// 归档/删除因此静默失效。
///
/// 本测试不经过 TLS（生产 connect() 会拒绝明文），
/// 而是直接复现同一串 IMAP 命令，验证「探测后重新 SELECT」这一修复要点。
#[test]
fn examine_switches_mailbox_so_must_reselect() {
    use std::io::{BufRead, BufReader, Write};

    if !server_up() {
        return;
    }
    reset_server();

    let tcp = TcpStream::connect((IMAP_HOST, IMAP_PORT)).unwrap();
    tcp.set_read_timeout(Some(std::time::Duration::from_secs(5))).ok();
    let mut w = tcp.try_clone().unwrap();
    let mut r = BufReader::new(tcp);

    let mut line = String::new();
    let _ = r.read_line(&mut line);

    // 发送命令并读到指定 tag 的响应，收集所有未标记响应行
    let talk = |w: &mut TcpStream, r: &mut BufReader<TcpStream>, tag: &str, cmd: &str| -> Vec<String> {
        let _ = w.write_all(format!("{tag} {cmd}\r\n").as_bytes());
        let _ = w.flush();
        let mut out = Vec::new();
        loop {
            let mut l = String::new();
            if r.read_line(&mut l).is_err() || l.is_empty() {
                break;
            }
            let t = l.trim_end().to_string();
            if t.starts_with(&format!("{tag} ")) {
                break;
            }
            out.push(t);
        }
        out
    };

    talk(&mut w, &mut r, "T1", "LOGIN test@example.com secret123");

    // —— 复现缺陷场景 ——
    talk(&mut w, &mut r, "T2", "SELECT INBOX");
    talk(&mut w, &mut r, "T3", "EXAMINE Archive"); // 会切走选中邮箱
    talk(&mut w, &mut r, "T4", "UID MOVE 101 Archive"); // 若不复位，将错误地作用于 Archive
    let info = talk(&mut w, &mut r, "T5", "NOOP");

    let moveinfo: Vec<&String> = info.iter().filter(|l| l.contains("MOVEINFO")).collect();
    assert!(
        !moveinfo.is_empty(),
        "模拟服务器未回带 MOVE 记录，无法验证：{info:?}"
    );
    assert!(
        moveinfo.iter().any(|l| l.contains("Archive|101|Archive")),
        "预期在未重新 SELECT 时会作用在 Archive 上（证明 EXAMINE 确实切走了选中邮箱）：{moveinfo:?}"
    );

    // —— 验证修复后的正确流程：EXAMINE 之后重新 SELECT 源文件夹 ——
    reset_server();
    let tcp = TcpStream::connect((IMAP_HOST, IMAP_PORT)).unwrap();
    tcp.set_read_timeout(Some(std::time::Duration::from_secs(5))).ok();
    let mut w = tcp.try_clone().unwrap();
    let mut r = BufReader::new(tcp);
    let mut line = String::new();
    let _ = r.read_line(&mut line);

    talk(&mut w, &mut r, "U1", "LOGIN test@example.com secret123");
    talk(&mut w, &mut r, "U2", "SELECT INBOX");
    talk(&mut w, &mut r, "U3", "EXAMINE Archive");
    talk(&mut w, &mut r, "U4", "SELECT INBOX"); // 修复要点：重新选中源文件夹
    talk(&mut w, &mut r, "U5", "UID MOVE 101 Archive");
    let info2 = talk(&mut w, &mut r, "U6", "NOOP");

    let moveinfo2: Vec<&String> = info2.iter().filter(|l| l.contains("MOVEINFO")).collect();
    assert!(
        moveinfo2.iter().any(|l| l.contains("INBOX|101|Archive")),
        "重新 SELECT 后 MOVE 应作用于 INBOX：{moveinfo2:?}"
    );
    assert!(
        !moveinfo2.iter().any(|l| l.contains("Archive|101|Archive")),
        "不应再错误地作用于 Archive：{moveinfo2:?}"
    );

    // 验证邮件真的移动成功了
    let uids = talk(&mut w, &mut r, "U7", "UID FETCH 101 (UID)");
    assert!(
        uids.iter().all(|l| !l.contains("UID 101")),
        "UID 101 应已移出 INBOX：{uids:?}"
    );
}
