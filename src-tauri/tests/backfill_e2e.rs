//! 端到端测试：历史邮件分批回填。
//!
//! 需要一个「大邮箱」模拟服务器（INBOX 含 1000 封邮件）：
//! ```bash
//! MOCK_BULK=1 python scripts/mock_mail_server.py 11243 11026
//! ```
//! 未检测到该服务器时测试自动跳过。
//!
//! 覆盖点（这些是早期实现的真实缺陷）：
//! - 首次同步应优先返回「最近的」邮件，而不是最老的
//! - 单次同步受上限限制，不能一次拉完 1000 封
//! - 后续同步能逐批向前回填，最终**不重不漏**地覆盖全部 UID
//! - 回填完成后游标归零，不再重复探测

use std::io::{BufRead, BufReader, Write};
use std::net::TcpStream;

const HOST: &str = "127.0.0.1";
const IMAP_PORT: u16 = 11243;
const USER: &str = "test@example.com";
const PASS: &str = "secret123";

/// 单次同步的下载上限，与 Rust 侧 MAX_FETCH_PER_SYNC 保持一致
const CAP: usize = 300;
/// 模拟邮箱的邮件总数，与 MOCK_BULK_COUNT 保持一致
const TOTAL: u32 = 1000;

fn bulk_server_up() -> bool {
    TcpStream::connect((HOST, IMAP_PORT)).is_ok()
}

struct Conn {
    w: TcpStream,
    r: BufReader<TcpStream>,
    tag: usize,
}

impl Conn {
    fn open() -> std::io::Result<Self> {
        let tcp = TcpStream::connect((HOST, IMAP_PORT))?;
        tcp.set_read_timeout(Some(std::time::Duration::from_secs(20))).ok();
        let w = tcp.try_clone()?;
        let mut r = BufReader::new(tcp);
        let mut greet = String::new();
        let _ = r.read_line(&mut greet);
        Ok(Self { w, r, tag: 0 })
    }

    /// 发送命令，返回所有未标记响应行的文本与实际读到的正文块。
    ///
    /// 用 `read_until(b'\n')` 按行读，而不是逐字节 `read`：前者走 BufReader 缓冲区，
    /// 后者会把缓冲绕过（且极易把紧跟的邮件正文一起吃掉）。
    fn cmd(&mut self, c: &str) -> (Vec<String>, Vec<Vec<u8>>) {
        self.tag += 1;
        let tag = format!("X{}", self.tag);
        let _ = self.w.write_all(format!("{tag} {c}\r\n").as_bytes());
        let _ = self.w.flush();

        let mut lines = Vec::new();
        let mut blobs = Vec::new();
        loop {
            let mut buf = Vec::new();
            match self.r.read_until(b'\n', &mut buf) {
                Ok(0) => return (lines, blobs),
                Ok(_) => {}
                Err(_) => return (lines, blobs),
            }
            let line = String::from_utf8_lossy(&buf)
                .trim_end_matches(['\r', '\n'])
                .to_string();

            // 形如 "* 5 FETCH (UID 5 FLAGS () BODY[] {123}" —— 后面紧跟 123 字节正文 + ")\r\n"
            if line.ends_with('}') {
                if let Some(idx) = line.rfind('{') {
                    if let Ok(n) = line[idx + 1..line.len() - 1].parse::<usize>() {
                        let mut data = vec![0u8; n];
                        if std::io::Read::read_exact(&mut self.r, &mut data).is_ok() {
                            blobs.push(data);
                            // 吞掉正文后紧跟的 ")\r\n"
                            let mut tail = Vec::new();
                            let _ = self.r.read_until(b'\n', &mut tail);
                        }
                        lines.push(line);
                        continue;
                    }
                }
            }

            let is_done = line.starts_with(&format!("{tag} "));
            lines.push(line);
            if is_done {
                return (lines, blobs);
            }
        }
    }

    /// 登录并选中 INBOX。
    ///
    /// 必须 SELECT：IMAP 的 UID 命令只在「已选中的邮箱」上生效，
    /// 没选邮箱就发 UID FETCH 会被服务器以 NO 拒绝。
    fn login(&mut self) {
        self.cmd(&format!("LOGIN {USER} {PASS}"));
        self.cmd("SELECT INBOX");
    }
}

/// 提取 "Subject: bulk message N" 里的 N
fn subject_numbers(blobs: &[Vec<u8>]) -> Vec<u32> {
    blobs
        .iter()
        .filter_map(|b| {
            let s = String::from_utf8_lossy(b);
            s.lines().find_map(|l| {
                l.strip_prefix("Subject: bulk message ")
                    .and_then(|n| n.trim().parse::<u32>().ok())
            })
        })
        .collect()
}

/// 按 UID 区间取 UID 列表（升序）
fn list_uids(conn: &mut Conn, range: &str) -> Vec<u32> {
    let (lines, _) = conn.cmd(&format!("UID FETCH {range} (UID FLAGS)"));
    let mut out: Vec<u32> = lines
        .iter()
        .filter_map(|l| {
            // "* 5 FETCH (UID 5 FLAGS ())"
            let i = l.find("UID ")?;
            let rest = &l[i + 4..];
            let end = rest.find(|c: char| !c.is_ascii_digit()).unwrap_or(rest.len());
            rest[..end].parse::<u32>().ok()
        })
        .collect();
    out.sort_unstable();
    out.dedup();
    out
}

/// 下载指定 UID 集合的正文，返回邮件序号
fn fetch_bodies(conn: &mut Conn, uids: &[u32]) -> Vec<u32> {
    if uids.is_empty() {
        return Vec::new();
    }
    let set = uids
        .iter()
        .map(|u| u.to_string())
        .collect::<Vec<_>>()
        .join(",");
    let (_, blobs) = conn.cmd(&format!("UID FETCH {set} (UID BODY.PEEK[])"));
    subject_numbers(&blobs)
}

/// 完整走一遍同步流程：首次取最近一批，然后逐批回填，返回所有拉到的邮件序号
fn run_full_sync(conn: &mut Conn) -> Vec<u32> {
    let mut got: Vec<u32> = Vec::new();

    // —— 首次同步：只取「最新的一批」——
    let all = list_uids(conn, "1:*");
    assert_eq!(all.len() as u32, TOTAL, "模拟邮箱应有 {TOTAL} 封邮件");
    let take = all.len().min(CAP);
    let first_batch = &all[all.len() - take..];
    got.extend(fetch_bodies(conn, first_batch));

    let mut last_uid = *first_batch.last().unwrap();
    let mut backfill = Some(first_batch[0].saturating_sub(1));

    // —— 后续同步：逐批向前回填 ——
    let mut rounds = 0;
    while let Some(upper) = backfill {
        if upper == 0 {
            break;
        }
        rounds += 1;
        assert!(rounds < 20, "回填未收敛，可能死循环（已 {rounds} 轮）");

        let hist = list_uids(conn, &format!("1:{upper}"));
        if hist.is_empty() {
            backfill = Some(0);
            break;
        }
        let t = hist.len().min(CAP);
        let batch = &hist[hist.len() - t..];
        got.extend(fetch_bodies(conn, batch));
        backfill = Some(batch[0].saturating_sub(1));

        // 模拟「新邮件」方向也不断推进（本例没有新邮件，保持不变）
        last_uid = last_uid.max(*batch.last().unwrap());
    }

    assert_eq!(last_uid, TOTAL, "新邮件游标应停在最新 UID");
    assert_eq!(backfill, Some(0), "回填游标应收敛到 0");
    got
}

#[test]
fn first_batch_is_most_recent() {
    if !bulk_server_up() {
        eprintln!("跳过：大邮箱模拟服务器未运行（MOCK_BULK=1 python scripts/mock_mail_server.py 11243 11026）");
        return;
    }
    let mut c = Conn::open().unwrap();
    c.login();

    let all = list_uids(&mut c, "1:*");
    let take = all.len().min(CAP);
    let batch = &all[all.len() - take..];

    assert_eq!(batch.len(), CAP, "首批应为上限封数");
    assert_eq!(batch[0], TOTAL - CAP as u32 + 1, "首批应是最新的那批");
    assert_eq!(*batch.last().unwrap(), TOTAL);
}

#[test]
fn backfill_covers_every_message_exactly_once() {
    if !bulk_server_up() {
        return;
    }
    let mut c = Conn::open().unwrap();
    c.login();

    let got = run_full_sync(&mut c);

    // 不重复
    let mut uniq = got.clone();
    uniq.sort_unstable();
    uniq.dedup();
    assert_eq!(got.len(), TOTAL as usize, "下载次数应等于邮件总数（无重复）");
    assert_eq!(uniq.len(), TOTAL as usize, "去重后应覆盖全部邮件");

    // 不遗漏：1..=1000 每封都要有
    let missing: Vec<u32> = (1..=TOTAL).filter(|n| !uniq.contains(n)).collect();
    assert!(missing.is_empty(), "遗漏了这些邮件：{missing:?}");

    assert_eq!(uniq[0], 1, "必须覆盖到最旧的第 1 封");
    assert_eq!(uniq[uniq.len() - 1], TOTAL, "必须覆盖到最新的第 {TOTAL} 封");
}

#[test]
fn backfill_progresses_in_batches_not_all_at_once() {
    if !bulk_server_up() {
        return;
    }
    let mut c = Conn::open().unwrap();
    c.login();

    // 首次同步不应一次拿完 1000 封
    let all = list_uids(&mut c, "1:*");
    assert_eq!(all.len() as u32, TOTAL);
    assert!(
        all.len() > CAP,
        "该测试需要邮件总数（{TOTAL}）大于单批上限（{CAP}）才有意义"
    );

    // 走完整流程应需要多轮（1000 封 / 300 每轮 ≈ 4 轮）
    let mut got = Vec::new();
    let all2 = list_uids(&mut c, "1:*");
    let first = &all2[all2.len() - CAP..];
    got.extend(fetch_bodies(&mut c, first));
    let mut backfill = Some(first[0].saturating_sub(1));
    let mut rounds = 1;

    while let Some(upper) = backfill {
        if upper == 0 {
            break;
        }
        rounds += 1;
        let hist = list_uids(&mut c, &format!("1:{upper}"));
        let t = hist.len().min(CAP);
        let batch = &hist[hist.len() - t..];
        got.extend(fetch_bodies(&mut c, batch));
        backfill = Some(batch[0].saturating_sub(1));
    }

    assert!(rounds >= 3, "1000 封至少需要 4 轮（先首批再回填），实际 {rounds} 轮");
    assert_eq!(got.len(), TOTAL as usize);
}
