package core

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// stallServer 模拟 QQ 邮箱：先等一会儿再发问候，并丢弃问候前收到的所有命令；
// answerLogin 为 false 时，问候后的 LOGIN 也永远不回。
func stallServer(t *testing.T, answerLogin bool) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				time.Sleep(200 * time.Millisecond)
				// 丢弃问候前到达的数据
				_ = c.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
				_, _ = c.Read(make([]byte, 4096))
				_ = c.SetReadDeadline(time.Time{})
				_, _ = c.Write([]byte("* OK [CAPABILITY IMAP4rev1 AUTH=PLAIN] ready\r\n"))
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					tag, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
					if strings.HasPrefix(strings.ToUpper(rest), "LOGIN") {
						if !answerLogin {
							continue
						}
						_, _ = c.Write([]byte(tag + " NO [AUTHENTICATIONFAILED] Login fail.\r\n"))
						continue
					}
					_, _ = c.Write([]byte(tag + " OK done\r\n"))
				}
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func withInsecure(t *testing.T) {
	allowInsecure = true
	t.Cleanup(func() { allowInsecure = false })
}

// 回归：QQ 邮箱丢弃问候前的命令，以前 LOGIN 永远等不到回复，添加邮箱卡在「正在验证」。
func TestDialWaitsForGreeting(t *testing.T) {
	withInsecure(t)
	cfg := mockCfg(stallServer(t, true), 1)
	done := make(chan error, 1)
	go func() { _, err := dial(cfg); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "认证失败") {
			t.Fatalf("应很快拿到认证失败：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LOGIN 在问候前发出被丢弃，dial 卡住了")
	}
}

func TestDialLoginTimeout(t *testing.T) {
	withInsecure(t)
	old := loginTimeout
	loginTimeout = 500 * time.Millisecond
	t.Cleanup(func() { loginTimeout = old })
	cfg := mockCfg(stallServer(t, false), 1)
	start := time.Now()
	_, err := dial(cfg)
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("应报登录超时：%v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("超时太慢：%v", time.Since(start))
	}
}
