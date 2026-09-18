#!/usr/bin/env python3
"""
极简 IMAP/SMTP 模拟服务器，用于端到端验证 Vela 的真实邮箱接入。

只实现 Vela 实际用到的那部分协议，但严格遵循 IMAP 语义——
特别是 SELECT/EXAMINE 会切换当前选中邮箱这一关键行为，
以便暴露「在错误的邮箱上执行 UID 命令」这类 bug。

用法：
    python mock_mail_server.py <imap_port> <smtp_port>
"""
import base64
import os
import socket
import socketserver
import sys
import threading
import time

IMAP_PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 11143
SMTP_PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 11025

USER = "test@example.com"
PASSWORD = "secret123"

USER = "test@example.com"
PASSWORD = "secret123"


# 大邮箱测试模式：设置 MOCK_BULK=1 时，INBOX 会包含 1000 封邮件，
# 用于验证「历史邮件分批回填」不会漏拉也不会重复。
BULK = os.environ.get("MOCK_BULK") == "1"
BULK_COUNT = int(os.environ.get("MOCK_BULK_COUNT", "1000"))


def _bulk_inbox():
    """生成 BULK_COUNT 封带连续 UID 的邮件，正文里带上序号便于断言"""
    box = {}
    for i in range(1, BULK_COUNT + 1):
        raw = (
            f"From: bulk{i}@example.com\r\n"
            f"To: test@example.com\r\n"
            f"Subject: bulk message {i}\r\n"
            f"Date: Mon, 01 Sep 2026 00:00:{i % 60:02d} +0800\r\n"
            f"Message-ID: <bulk{i}@example.com>\r\n"
            f"MIME-Version: 1.0\r\n"
            f"Content-Type: text/plain; charset=utf-8\r\n"
            f"\r\n"
            f"bulk body {i}\r\n"
        ).encode()
        box[i] = {"flags": {"\\Seen"} if i % 3 == 0 else set(), "raw": raw}
    return box


def fresh_boxes():
    """返回一份全新的邮箱数据（每次重置用，避免测试间互相污染）"""
    if BULK:
        return {
            "INBOX": _bulk_inbox(),
            "Sent Messages": {},
            "Archive": {},
            "Trash": {},
        }
    return {
        "INBOX": {
            101: {
                "flags": set(),
                "raw": (
                    b"From: =?GBK?B?1dXI/bLO?= <zhang@qq.com>\r\n"
                    b"To: test@example.com\r\n"
                    b"Subject: =?GBK?B?suLK1NPKvP4=?=\r\n"
                    b"Date: Thu, 18 Sep 2026 10:00:00 +0800\r\n"
                    b"Message-ID: <m101@qq.com>\r\n"
                    b"MIME-Version: 1.0\r\n"
                    b"Content-Type: text/plain; charset=\"GBK\"\r\n"
                    b"Content-Transfer-Encoding: base64\r\n"
                    b"\r\n"
                    b"xOO6w8rAvec=\r\n"
                ),
            },
            102: {
                "flags": {"\\Seen"},
                "raw": (
                    b"From: Alice <alice@example.org>\r\n"
                    b"To: test@example.com\r\n"
                    b"Subject: Plain ascii\r\n"
                    b"Date: Fri, 19 Sep 2026 11:30:00 +0800\r\n"
                    b"Message-ID: <m102@example.org>\r\n"
                    b"MIME-Version: 1.0\r\n"
                    b"Content-Type: text/plain; charset=utf-8\r\n"
                    b"\r\n"
                    b"Hello there.\r\n"
                ),
            },
        },
        "Sent Messages": {
            201: {
                "flags": {"\\Seen"},
                "raw": (
                    b"From: test@example.com\r\n"
                    b"To: bob@example.org\r\n"
                    b"Subject: Re: hi\r\n"
                    b"Date: Sat, 20 Sep 2026 09:00:00 +0800\r\n"
                    b"Message-ID: <m201@example.com>\r\n"
                    b"MIME-Version: 1.0\r\n"
                    b"Content-Type: text/plain; charset=utf-8\r\n"
                    b"\r\n"
                    b"Already sent.\r\n"
                ),
            }
        },
        "Archive": {},
        "Trash": {},
    }


# 每个文件夹的邮件：uid -> {flags, raw}
MAILBOXES = fresh_boxes()

# 记录收到的 IMAP 命令，便于测试断言
COMMAND_LOG = []
# 记录 (实际作用的邮箱, uid集合, 目标邮箱)，供测试断言
MOVE_LOG = []
# 记录 SMTP 收到的邮件正文
SMTP_RECEIVED = []


def _fmt_flags(flags):
    if not flags:
        return ""
    return " " + " ".join(sorted(flags))


class ImapHandler(socketserver.StreamRequestHandler):
    def handle(self):
        self.selected = None          # 关键：当前选中的邮箱名
        self.authenticated = False
        self.send(b"* OK [CAPABILITY IMAP4rev1 MOVE UIDPLUS] mock ready\r\n")
        while True:
            line = self.rfile.readline()
            if not line:
                break
            text = line.decode("utf-8", "replace").rstrip("\r\n")
            if not text:
                continue
            COMMAND_LOG.append(text)
            if not self.dispatch(text):
                break

    def send(self, data):
        self.wfile.write(data if isinstance(data, bytes) else data.encode())

    def dispatch(self, text):
        parts = text.split(" ", 2)
        if len(parts) < 2:
            return True
        tag = parts[0]
        cmd = parts[1].upper()
        rest = parts[2] if len(parts) > 2 else ""

        if cmd == "CAPABILITY":
            self.send("* CAPABILITY IMAP4rev1 MOVE UIDPLUS\r\n")
            self.send(f"{tag} OK CAPABILITY completed\r\n")

        elif cmd == "LOGIN":
            try:
                user, pw = rest.rsplit(" ", 1)
                user = user.strip('"')
                pw = pw.strip('"')
            except ValueError:
                self.send(f"{tag} BAD malformed LOGIN\r\n")
                return True
            if user == USER and pw == PASSWORD:
                self.authenticated = True
                self.send(f"{tag} OK LOGIN completed\r\n")
            else:
                self.send(f"{tag} NO [AUTHENTICATIONFAILED] invalid credentials\r\n")

        elif cmd == "LIST":
            self.send("* LIST (\\HasNoChildren) \"/\" \"INBOX\"\r\n")
            for name in MAILBOXES:
                if name != "INBOX":
                    self.send(f'* LIST (\\HasNoChildren) "/" "{name}"\r\n')
            self.send(f"{tag} OK LIST completed\r\n")

        elif cmd in ("SELECT", "EXAMINE"):
            name = rest.strip().strip('"')
            # IMAP 大小写不敏感，INBOX 尤甚
            real = None
            for k in MAILBOXES:
                if k.upper() == name.upper():
                    real = k
                    break
            if real is None:
                self.send(f"{tag} NO Mailbox doesn't exist: {name}\r\n")
                return True
            # 关键语义：SELECT 与 EXAMINE 都会切换当前选中邮箱
            self.selected = real
            box = MAILBOXES[real]
            self.send(f"* {len(box)} EXISTS\r\n")
            self.send("* FLAGS (\\Seen \\Answered \\Flagged \\Deleted \\Draft)\r\n")
            self.send("* OK [UIDVALIDITY 1] UIDs valid\r\n")
            nxt = max(box.keys()) + 1 if box else 1
            self.send(f"* OK [UIDNEXT {nxt}] Predicted next UID\r\n")
            ro = " (READ-ONLY)" if cmd == "EXAMINE" else ""
            self.send(f"{tag} OK [READ-WRITE] {cmd} completed{ro}\r\n")

        elif cmd == "UID":
            return self.handle_uid(tag, rest)

        elif cmd == "CREATE":
            name = rest.strip().strip('"')
            MAILBOXES.setdefault(name, {})
            self.send(f"{tag} OK CREATE completed\r\n")

        elif cmd == "EXPUNGE":
            self.send(f"{tag} OK EXPUNGE completed\r\n")

        elif cmd == "NOOP":
            # 测试辅助：NOOP 响应里回带 MOVE 调用记录，便于断言「MOVE 实际作用在哪个邮箱」
            for box_name, uid_set, target in MOVE_LOG:
                self.send(f"* MOVEINFO {box_name}|{uid_set}|{target}\r\n")
            self.send(f"{tag} OK NOOP completed\r\n")

        elif cmd == "RESET":
            # 测试专用：把邮箱恢复到初始状态，使各测试互不依赖执行顺序
            MAILBOXES.clear()
            MAILBOXES.update(fresh_boxes())
            MOVE_LOG.clear()
            self.selected = None
            self.send(f"{tag} OK RESET completed\r\n")

        elif cmd == "LOGOUT":
            self.send("* BYE logging out\r\n")
            self.send(f"{tag} OK LOGOUT completed\r\n")
            return False

        else:
            self.send(f"{tag} BAD unknown command {cmd}\r\n")
        return True

    def handle_uid(self, tag, rest):
        if not self.authenticated:
            self.send(f"{tag} NO not authenticated\r\n")
            return True

        sub, _, args = rest.partition(" ")
        sub = sub.upper()

        # 验证：UID 命令必须作用于当前选中的邮箱
        if self.selected is None:
            self.send(f"{tag} NO no mailbox selected\r\n")
            return True

        if sub == "FETCH":
            uid_set, _, query = args.partition(" ")
            box = MAILBOXES[self.selected]
            uids = self.parse_uid_set(uid_set, box)
            for uid in uids:
                item = box[uid]
                # 只有显式要求 BODY[]/RFC822 才返回正文；BODY.PEEK[] 不得置 \Seen
                wants_body = "BODY.PEEK[]" in query.upper() or "BODY[]" in query.upper()
                if "BODY[]" in query.upper() and "PEEK" not in query.upper():
                    item["flags"].add("\\Seen")
                flags = _fmt_flags(item["flags"])
                if wants_body:
                    raw = item["raw"]
                    self.send(
                        f"* {uid} FETCH (UID {uid} FLAGS ({flags.strip()}) "
                        f"BODY[] {{{len(raw)}}}\r\n"
                    )
                    self.wfile.write(raw)
                    self.send(b")\r\n")
                else:
                    self.send(f"* {uid} FETCH (UID {uid} FLAGS ({flags.strip()}))\r\n")
            self.send(f"{tag} OK UID FETCH completed\r\n")

        elif sub == "STORE":
            uid_set, _, spec = args.partition(" ")
            box = MAILBOXES[self.selected]
            uids = self.parse_uid_set(uid_set, box)
            add = spec.strip().upper().startswith("+")
            for uid in uids:
                for flag in ("\\SEEN", "\\FLAGGED", "\\DELETED"):
                    if flag in spec.upper():
                        if add:
                            box[uid]["flags"].add(flag)
                        else:
                            box[uid]["flags"].discard(flag)
                self.send(f"* {uid} FETCH (FLAGS ({_fmt_flags(box[uid]['flags']).strip()}))\r\n")
            self.send(f"{tag} OK UID STORE completed\r\n")

        elif sub in ("COPY", "MOVE"):
            uid_set, _, target = args.partition(" ")
            target = target.strip().strip('"')
            # 记录本次 MOVE/COPY 作用于哪个邮箱，暴露「EXAMINE 切换选中邮箱」类 bug
            MOVE_LOG.append((self.selected, uid_set.strip(), target))
            box = MAILBOXES[self.selected]
            uids = self.parse_uid_set(uid_set, box)
            if target not in MAILBOXES:
                self.send(f"{tag} NO [TRYCREATE] target missing\r\n")
                return True
            dst = MAILBOXES[target]
            nxt = max(dst.keys()) + 1 if dst else 1
            for uid in uids:
                dst[nxt] = {
                    "flags": set(box[uid]["flags"]),
                    "raw": box[uid]["raw"],
                }
                nxt += 1
                if sub == "MOVE":
                    del box[uid]
            self.send(f"{tag} OK UID {sub} completed\r\n")

        elif sub == "EXPUNGE":
            uid_set, _, _ = args.partition(" ")
            box = MAILBOXES[self.selected]
            uids = self.parse_uid_set(uid_set, box)
            for uid in uids:
                if "\\Deleted" in box.get(uid, {}).get("flags", set()):
                    del box[uid]
            self.send(f"{tag} OK UID EXPUNGE completed\r\n")

        else:
            self.send(f"{tag} BAD unknown UID subcommand {sub}\r\n")
        return True

    @staticmethod
    def parse_uid_set(spec, box):
        """支持 '1:*'、'a,b,c'、单个 uid"""
        spec = spec.strip()
        out = []
        if not box and spec != "1:*":
            return []
        for part in spec.split(","):
            if ":" in part:
                lo, _, hi = part.partition(":")
                lo = int(lo) if lo.isdigit() else 1
                hi = max(box.keys()) if (hi == "*" and box) else (int(hi) if hi.isdigit() else 0)
                out.extend(u for u in sorted(box) if lo <= u <= hi)
            elif part.isdigit():
                u = int(part)
                if u in box:
                    out.append(u)
        return sorted(set(out))


class ThreadedTCPServer(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


class SmtpHandler(socketserver.StreamRequestHandler):
    def handle(self):
        self.send(b"220 mock smtp\r\n")
        data_mode = False
        buffer = []

        while True:
            line = self.rfile.readline()
            if not line:
                break
            if data_mode:
                if line.rstrip(b"\r\n") == b".":
                    data_mode = False
                    SMTP_RECEIVED.append(b"".join(buffer))
                    buffer = []
                    self.send(b"250 OK queued\r\n")
                else:
                    buffer.append(line)
                continue

            text = line.decode("utf-8", "replace").strip()
            upper = text.upper()
            if upper.startswith(("EHLO", "HELO")):
                # 不宣告 STARTTLS：测试用明文（Security::None 在业务层已被拒绝，
                # 这里只验证 SMTP 命令序列本身）
                self.send(b"250-mock\r\n250 AUTH LOGIN PLAIN\r\n")
            elif upper.startswith("AUTH LOGIN"):
                self.send(b"334 VXNlcm5hbWU6\r\n")
                self.rfile.readline()
                self.send(b"334 UGFzc3dvcmQ6\r\n")
                self.rfile.readline()
                self.send(b"235 authenticated\r\n")
            elif upper.startswith("AUTH PLAIN"):
                self.send(b"235 authenticated\r\n")
            elif upper.startswith("MAIL FROM"):
                self.send(b"250 OK\r\n")
            elif upper.startswith("RCPT TO"):
                self.send(b"250 OK\r\n")
            elif upper.startswith("DATA"):
                data_mode = True
                self.send(b"354 send data\r\n")
            elif upper.startswith("QUIT"):
                self.send(b"221 bye\r\n")
                break
            else:
                self.send(b"250 OK\r\n")

    def send(self, data):
        self.wfile.write(data)


def main():
    imap = ThreadedTCPServer(("127.0.0.1", IMAP_PORT), ImapHandler)
    smtp = ThreadedTCPServer(("127.0.0.1", SMTP_PORT), SmtpHandler)
    threading.Thread(target=imap.serve_forever, daemon=True).start()
    threading.Thread(target=smtp.serve_forever, daemon=True).start()
    print(f"IMAP ready on 127.0.0.1:{IMAP_PORT}", flush=True)
    print(f"SMTP ready on 127.0.0.1:{SMTP_PORT}", flush=True)
    while True:
        time.sleep(1)


if __name__ == "__main__":
    main()
