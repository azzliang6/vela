package core

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// MaxFetchPerSync 是单个文件夹单次同步最多下载的邮件数（与 Rust 版相同）。
const MaxFetchPerSync = 300

// MaxBodyChars 是正文保留的最大字符数。
const MaxBodyChars = 200_000

// allowInsecure 只在测试里打开，让协议层连接明文的模拟服务器。
var allowInsecure = false

// FetchResult 是一次拉取的结果。
type FetchResult struct {
	Mails   []Email
	Cursors []MailboxCursor
	Skipped int
	Present []FolderUIDs
}

type session struct {
	c       *imapclient.Client
	updates chan struct{}
}

func dial(cfg MailServerConfig) (*session, error) {
	s := &session{updates: make(chan struct{}, 1)}
	opts := &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: cfg.IMAPHost},
		Dialer:    &net.Dialer{Timeout: 30 * time.Second},
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(d *imapclient.UnilateralDataMailbox) {
				if d.NumMessages != nil {
					select {
					case s.updates <- struct{}{}:
					default:
					}
				}
			},
		},
	}
	addr := net.JoinHostPort(cfg.IMAPHost, fmt.Sprint(cfg.IMAPPort))
	var err error
	switch cfg.IMAPSecurity {
	case SecuritySSL:
		s.c, err = imapclient.DialTLS(addr, opts)
		if err != nil {
			return nil, fmt.Errorf("连接 %s:%d 失败：%v", cfg.IMAPHost, cfg.IMAPPort, err)
		}
	case SecurityStartTLS:
		s.c, err = imapclient.DialStartTLS(addr, opts)
		if err != nil {
			return nil, fmt.Errorf("STARTTLS 连接 %s:%d 失败：%v", cfg.IMAPHost, cfg.IMAPPort, err)
		}
	default:
		if !allowInsecure {
			return nil, errors.New("出于安全考虑不支持无加密的 IMAP；请使用 993(SSL) 或 143(STARTTLS)")
		}
		s.c, err = imapclient.DialInsecure(addr, opts)
		if err != nil {
			return nil, fmt.Errorf("连接 %s:%d 失败：%v", cfg.IMAPHost, cfg.IMAPPort, err)
		}
	}
	if err := s.c.Login(cfg.Email, cfg.Password).Wait(); err != nil {
		s.c.Close()
		return nil, loginError(err)
	}
	return s, nil
}

func (s *session) close() {
	_ = s.c.Logout().Wait()
	_ = s.c.Close()
}

func loginError(err error) error {
	text := err.Error()
	lower := strings.ToLower(text)
	if strings.Contains(lower, "auth") || strings.Contains(lower, "invalid credentials") || strings.Contains(lower, "login failed") {
		return fmt.Errorf("认证失败：请确认使用的是「授权码 / 应用专用密码」而非网页登录密码（服务器返回：%s）", text)
	}
	return fmt.Errorf("IMAP 登录失败：%s", text)
}

func (s *session) exists(name string) bool {
	_, err := s.c.Select(name, &imap.SelectOptions{ReadOnly: true}).Wait()
	return err == nil
}

func (s *session) resolve(kind FolderKind) string {
	for _, n := range FolderCandidates(kind) {
		if s.exists(n) {
			return n
		}
	}
	return ""
}

type uidMeta struct {
	uid             uint32
	seen, flagged   bool
}

func hasFlag(flags []imap.Flag, want imap.Flag) bool {
	for _, f := range flags {
		if strings.EqualFold(string(f), string(want)) {
			return true
		}
	}
	return false
}

// listUIDs 列出 [lo, hi] 区间的 UID 与已读/星标（hi 为 0 表示 *）。
func (s *session) listUIDs(lo, hi uint32, folder string) ([]uidMeta, error) {
	set := imap.UIDSet{imap.UIDRange{Start: imap.UID(lo), Stop: imap.UID(hi)}}
	msgs, err := s.c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		return nil, fmt.Errorf("拉取 %s 邮件列表失败：%v", folder, err)
	}
	var out []uidMeta
	for _, m := range msgs {
		if m.UID == 0 {
			continue
		}
		out = append(out, uidMeta{uint32(m.UID), hasFlag(m.Flags, imap.FlagSeen), hasFlag(m.Flags, imap.FlagFlagged)})
	}
	slices.SortFunc(out, func(a, b uidMeta) int { return int(a.uid) - int(b.uid) })
	out = slices.CompactFunc(out, func(a, b uidMeta) bool { return a.uid == b.uid })
	return out, nil
}

func (s *session) download(folder, account, serverFolder string, items []uidMeta, out *FetchResult) error {
	if len(items) == 0 {
		return nil
	}
	var set imap.UIDSet
	meta := map[uint32]uidMeta{}
	for _, it := range items {
		set.AddNum(imap.UID(it.uid))
		meta[it.uid] = it
	}
	section := &imap.FetchItemBodySection{Peek: true}
	msgs, err := s.c.Fetch(set, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
	if err != nil {
		return fmt.Errorf("下载 %s 邮件正文失败：%v", folder, err)
	}
	for _, m := range msgs {
		raw := m.FindBodySection(section)
		if m.UID == 0 || raw == nil {
			out.Skipped++
			continue
		}
		mt := meta[uint32(m.UID)]
		mail, err := ParseMessage(raw, account, serverFolder, uint32(m.UID), mt.seen, mt.flagged, folder)
		if err != nil {
			out.Skipped++
			continue
		}
		out.Mails = append(out.Mails, mail)
	}
	return nil
}

func (s *session) fetchFolder(folder, account, serverFolder string, cur *MailboxCursor, out *FetchResult) error {
	sel, err := s.c.Select(folder, nil).Wait()
	if err != nil {
		return fmt.Errorf("无法打开文件夹 %s：%v", folder, err)
	}
	uv := sel.UIDValidity
	var since uint32
	var expectUV uint32
	var backfill *uint32
	if cur != nil {
		since, expectUV, backfill = cur.LastUID, cur.UIDValidity, cur.BackfillUID
	}
	if expectUV != 0 && uv != 0 && expectUV != uv { // 邮箱被重建，旧 UID 作废
		since, backfill = 0, nil
	}
	cap := MaxFetchPerSync

	if since == 0 && backfill == nil { // 首次同步：取最新的一批
		all, err := s.listUIDs(1, 0, folder)
		if err != nil {
			return err
		}
		batch := all[len(all)-min(len(all), cap):]
		if err := s.download(folder, account, serverFolder, batch, out); err != nil {
			return err
		}
		c := MailboxCursor{Folder: serverFolder, UIDValidity: uv, RemoteName: folder, BackfillUID: u32p(0)}
		if len(batch) > 0 {
			c.LastUID = batch[len(batch)-1].uid
			c.BackfillUID = u32p(batch[0].uid - 1)
		}
		out.Cursors = append(out.Cursors, c)
		return nil
	}

	// 阶段一：新邮件。"n:*" 在没有更大 UID 时会返回最后一封，这里过滤掉。
	listed, err := s.listUIDs(since+1, 0, folder)
	if err != nil {
		return err
	}
	var newItems []uidMeta
	for _, it := range listed {
		if it.uid > since {
			newItems = append(newItems, it)
		}
	}
	newDone := len(newItems) <= cap
	newBatch := newItems[:min(len(newItems), cap)]
	before := len(out.Mails)
	if err := s.download(folder, account, serverFolder, newBatch, out); err != nil {
		return err
	}
	if serverFolder == "inbox" {
		for i := before; i < len(out.Mails); i++ {
			out.Mails[i].IsNewArrival = out.Mails[i].UID > since && !out.Mails[i].Read
		}
	}
	nextLast := since
	for _, it := range newBatch {
		nextLast = max(nextLast, it.uid)
	}

	// 阶段二：历史回填
	var upper uint32
	switch {
	case backfill != nil:
		upper = *backfill
	case len(newItems) > 0:
		upper = newItems[0].uid - 1
	}
	nextBackfill := backfill
	if newDone {
		if upper == 0 {
			nextBackfill = u32p(0)
		} else {
			hist, err := s.listUIDs(1, upper, folder)
			if err != nil {
				return err
			}
			var filtered []uidMeta
			for _, h := range hist {
				if h.uid <= upper {
					filtered = append(filtered, h)
				}
			}
			if len(filtered) == 0 {
				nextBackfill = u32p(0)
			} else {
				batch := filtered[len(filtered)-min(len(filtered), cap):]
				if err := s.download(folder, account, serverFolder, batch, out); err != nil {
					return err
				}
				nextBackfill = u32p(batch[0].uid - 1)
			}
		}
	}
	out.Cursors = append(out.Cursors, MailboxCursor{Folder: serverFolder, LastUID: nextLast,
		BackfillUID: nextBackfill, UIDValidity: uv, RemoteName: folder})
	return nil
}

func findCursor(cs []MailboxCursor, folder string) *MailboxCursor {
	for i := range cs {
		if cs[i].Folder == folder {
			return &cs[i]
		}
	}
	return nil
}

// SyncAccount 拉取收件箱和已发送的新邮件 + 一批历史邮件。
func SyncAccount(cfg MailServerConfig, cursors []MailboxCursor) (*FetchResult, error) {
	s, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	defer s.close()
	res := &FetchResult{}
	if err := s.fetchFolder("INBOX", cfg.Email, "inbox", findCursor(cursors, "inbox"), res); err != nil {
		return nil, err
	}
	all, err := s.listUIDs(1, 0, "INBOX")
	if err != nil {
		return nil, err
	}
	res.Present = append(res.Present, FolderUIDs{"inbox", uidsOf(all)})
	if sent := s.resolve(FolderSent); sent != "" {
		if err := s.fetchFolder(sent, cfg.Email, "sent", findCursor(cursors, "sent"), res); err != nil {
			return nil, err
		}
		if _, err := s.c.Select(sent, nil).Wait(); err != nil {
			return nil, err
		}
		all, err := s.listUIDs(1, 0, sent)
		if err != nil {
			return nil, err
		}
		res.Present = append(res.Present, FolderUIDs{"sent", uidsOf(all)})
	}
	return res, nil
}

func uidsOf(m []uidMeta) []uint32 {
	out := make([]uint32, len(m))
	for i, x := range m {
		out[i] = x.uid
	}
	return out
}

// IdleFetchNew 保持连接，等到收件箱有新邮件后拉取返回（IDLE，不支持时 60 秒轮询）。
// stop 关闭时返回 nil, nil。
func IdleFetchNew(cfg MailServerConfig, cursors []MailboxCursor, idleWait time.Duration, stop <-chan struct{}) (*FetchResult, error) {
	s, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	defer s.close()
	idleCapable := s.c.Caps().Has(imap.CapIdle)
	for {
		res, err := s.fetchNewInbox(cfg.Email, cursors)
		if err != nil || res != nil {
			return res, err
		}
		if idleCapable {
			cmd, err := s.c.Idle()
			if err != nil {
				return nil, fmt.Errorf("进入 IDLE 失败：%v", err)
			}
			select {
			case <-s.updates:
			case <-time.After(idleWait):
			case <-stop:
				cmd.Close()
				return nil, nil
			case <-s.c.Closed():
				return nil, errors.New("IDLE 等待中断：连接已关闭")
			}
			if err := cmd.Close(); err != nil {
				return nil, fmt.Errorf("IDLE 等待中断：%v", err)
			}
		} else {
			select {
			case <-time.After(60 * time.Second):
			case <-stop:
				return nil, nil
			}
		}
	}
}

func (s *session) fetchNewInbox(account string, cursors []MailboxCursor) (*FetchResult, error) {
	c := findCursor(cursors, "inbox")
	if c == nil {
		return nil, nil
	}
	sel, err := s.c.Select("INBOX", nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("无法打开收件箱：%v", err)
	}
	if c.UIDValidity != 0 && sel.UIDValidity != 0 && c.UIDValidity != sel.UIDValidity {
		return nil, nil
	}
	items, err := s.listUIDs(c.LastUID+1, 0, "INBOX")
	if err != nil {
		return nil, err
	}
	var fresh []uidMeta
	for _, it := range items {
		if it.uid > c.LastUID {
			fresh = append(fresh, it)
		}
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	batch := fresh[:min(len(fresh), MaxFetchPerSync)]
	res := &FetchResult{}
	if err := s.download("INBOX", account, "inbox", batch, res); err != nil {
		return nil, err
	}
	last := c.LastUID
	for i := range res.Mails {
		res.Mails[i].IsNewArrival = !res.Mails[i].Read
	}
	for _, b := range batch {
		last = max(last, b.uid)
	}
	res.Cursors = append(res.Cursors, MailboxCursor{Folder: "inbox", LastUID: last, BackfillUID: c.BackfillUID,
		UIDValidity: sel.UIDValidity, RemoteName: "INBOX"})
	return res, nil
}

// ResolveServerFolder 探测某类文件夹在该邮箱里的真实名字，没有时返回 ""。
func ResolveServerFolder(cfg MailServerConfig, kind FolderKind) (string, error) {
	s, err := dial(cfg)
	if err != nil {
		return "", err
	}
	defer s.close()
	return s.resolve(kind), nil
}

// SetFlags 回写已读 / 星标。remoteName 为空时按已发送文件夹探测。
func SetFlags(cfg MailServerConfig, remoteName string, uid uint32, read, starred *bool) error {
	s, err := dial(cfg)
	if err != nil {
		return err
	}
	defer s.close()
	if remoteName == "" {
		remoteName = s.resolve(FolderSent)
	}
	if _, err := s.c.Select(remoteName, nil).Wait(); err != nil {
		return fmt.Errorf("无法打开文件夹 %s：%v", remoteName, err)
	}
	set := imap.UIDSetNum(imap.UID(uid))
	store := func(on bool, flag imap.Flag, what string) error {
		op := imap.StoreFlagsDel
		if on {
			op = imap.StoreFlagsAdd
		}
		if err := s.c.Store(set, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{flag}}, nil).Close(); err != nil {
			return fmt.Errorf("同步%s状态失败：%v", what, err)
		}
		return nil
	}
	if read != nil {
		if err := store(*read, imap.FlagSeen, "已读"); err != nil {
			return err
		}
	}
	if starred != nil {
		if err := store(*starred, imap.FlagFlagged, "星标"); err != nil {
			return err
		}
	}
	return nil
}

// MoveMessage 把邮件移到目标文件夹（不支持 MOVE 时 COPY + 删除）。
func MoveMessage(cfg MailServerConfig, remoteName string, uid uint32, kind FolderKind) error {
	s, err := dial(cfg)
	if err != nil {
		return err
	}
	defer s.close()
	target := s.resolve(kind)
	if target == "" {
		return errors.New("未在邮箱中找到对应的文件夹（归档 / 垃圾箱），该邮箱可能不支持")
	}
	if remoteName == "" {
		remoteName = s.resolve(FolderSent)
	}
	// 关键：探测会切换选中邮箱，移动前必须重新选中源文件夹
	if _, err := s.c.Select(remoteName, nil).Wait(); err != nil {
		return fmt.Errorf("无法重新打开文件夹 %s：%v", remoteName, err)
	}
	set := imap.UIDSetNum(imap.UID(uid))
	if s.c.Caps().Has(imap.CapMove) {
		if _, err := s.c.Move(set, target).Wait(); err != nil {
			return fmt.Errorf("移动到 %s 失败：%v", target, err)
		}
		return nil
	}
	if _, err := s.c.Copy(set, target).Wait(); err != nil {
		return fmt.Errorf("复制到 %s 失败：%v", target, err)
	}
	if err := s.c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return fmt.Errorf("标记删除失败：%v", err)
	}
	if err := s.c.UIDExpunge(set).Close(); err != nil {
		return fmt.Errorf("清理原邮件失败：%v", err)
	}
	return nil
}

// TestConnection 登录并返回文件夹列表，不写任何数据。
func TestConnection(cfg MailServerConfig) ([]string, error) {
	s, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	defer s.close()
	list, err := s.c.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("读取文件夹列表失败：%v", err)
	}
	var names []string
	for _, l := range list {
		names = append(names, l.Mailbox)
	}
	return names, nil
}
