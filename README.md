# Vela 轻帆

一款轻量桌面邮件客户端，支持 Windows / macOS / Linux。现在用 [MyGo](https://github.com/egoist/mygo) 构建：整个程序是 Go，界面是 MyGo 原生 UI（`ui` 包），只有 HTML 邮件正文放在内嵌 WebView 里显示。没有 Node / React / Tauri。

> Android 版本已停止维护（迁移到 MyGo 时移除）。

## 功能

- QQ / 163 / 126 / Gmail / Outlook / iCloud 等预设，也支持自定义 IMAP/SMTP；授权码用 AES-GCM 加密后存在本地
- 收件箱、已发送、草稿、归档、垃圾箱和智能分类；多账户
- 虚拟化邮件列表、搜索（⌘K / Ctrl+K）、星标、已读、归档、删除
- HTML 邮件放在沙箱 WebView 中显示：先清洗，再用 CSP 禁止所有脚本、表单和页内导航；链接在系统浏览器中打开
- 写信、回复、回复全部、转发，可存草稿
- 本地规则加上可选的 AI（OpenAI 兼容接口）分析分类
- IMAP IDLE 新邮件推送、系统通知和托盘（关闭窗口后隐藏到托盘）
- 浅色 / 深色 / 跟随系统；动画很克制，并会跟随系统的“减少动态效果”设置

## 开发

需要 Go（见 `go.mod`），不需要 cgo。

```sh
go tool mygo dev                    # 运行（原生 UI）
CGO_ENABLED=0 go test ./...         # 后端 + 界面测试（需要 python3，用来运行 scripts/mock_mail_server.py）
go tool mygo build                  # 打包当前平台
go tool mygo build -platform windows/amd64,linux/amd64   # 交叉打包（Windows 安装包需要 NSIS）
```

在 Linux 上，HTML 正文需要 WebKitGTK（`libwebkit2gtk-4.1-0`），托盘需要 `libayatana-appindicator3`。缺少前者时正文改为纯文本显示；缺少后者时没有托盘，关闭窗口即退出。

## 目录

- `main.go`：窗口、托盘、通知和 WebView 正文
- `internal/app`：原生界面（视图、状态、本地规则、HTML 清洗、动画）
- `internal/core`：邮件后端（IMAP 同步和 IDLE、SMTP、解析、SQLite、加密、AI），从原来的 Rust `src-tauri` 逐个命令移植
- `scripts/mock_mail_server.py`：测试用的 IMAP/SMTP 模拟服务器

## 数据兼容

继续使用 Tauri 版的数据目录，标识符都是 `app.vela.mail`：

| 内容 | 位置 |
| --- | --- |
| `vela.db`（SQLite） | Linux `~/.local/share/app.vela.mail`；macOS `~/Library/Application Support/app.vela.mail`；Windows `%APPDATA%\app.vela.mail` |
| `mail.key` | 与系统配置目录下的 `app.vela.mail` 相同 |

升级后已保存的账户和邮件可以直接用，表结构只是增加了几列（`uid`、`remote_name`、`message_id`）。`VELA_DATA_DIR` 会覆盖这两个目录，`VELA_SECRET` 会覆盖密钥。原来存在 WebView localStorage 里的界面设置（AI Key 等）不会迁移，需要重新设置一次。

## 备份与还原

设置里可以把邮箱账户（含授权码）、同步游标、本地邮件和偏好设置导出成一个用密码加密的 `.velabak` 文件，在另一台设备上还原。

- 还原不会更换本机密钥：授权码先用备份里的密钥材料解开，再用本机密钥（`mail.key`，或 `VELA_SECRET`）重新加密后写入，所以用 `VELA_SECRET` 的环境重启之后照常可用。
- 覆盖之前会把本机当前数据复制一份到数据目录下的 `snapshots/vela-before-restore-<时间>.db`，只保留最近 3 份。想撤销还原，退出 Vela 后用这份副本替换 `vela.db` 即可。
- 备份文件先压缩再加密（格式版本 2），通常只有未压缩时的几分之一。新版本能还原旧版导出的备份；旧版本的 Vela 读不了新格式，请先升级。
