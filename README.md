# Vela 轻帆 ⛵

**现代、美观、快速的跨平台邮件客户端** —— Tauri 2 + React 19 + TypeScript。

应用采用本地优先设计：桌面端数据保存在本机 SQLite，不需要注册账号或部署服务端；浏览器开发模式使用演示数据和 localStorage。

## 功能特性

- 📬 **三栏工作台**：账户/文件夹侧栏 · 邮件列表 · 沉浸阅读窗格
- 🧠 **AI 智能分类与摘要**：重要/财务/出行/社交/订阅/通知/垃圾七类自动归类，每封邮件一句话摘要，重要邮件附「为什么重要」；本地规则引擎离线运行，**发信即时分类**，未分类邮件可在阅读窗格一键「本地规则分析」
- 👥 **多账户管理**：3 个演示账户（Gmail / Outlook / QQ）+ 可接入真实邮箱，统一收件箱与单账户切换
- 📡 **真实邮箱接入**：桌面版支持 IMAP 收信 / SMTP 发信，已读星标归档删除均回写服务器（详见下文）
- 🔔 **来邮件桌面通知**：桌面版用 **IMAP IDLE 长连接**实时感知新邮件（秒级弹系统通知，无需等轮询），多封聚合成一条；可设「只提醒重要邮件」与免打扰时段；关窗后应用驻留系统托盘继续监控，真正退出走托盘菜单
- ⌨️ **键盘优先**：Gmail / Superhuman 风格快捷键 + `Ctrl+K` 命令面板（搜操作、搜邮件、跳转一切）
- ✅ **多选与批量操作**：hover 圆圈勾选、Ctrl+点选、Shift 范围选、`Ctrl+A` 全选；浮出批量条支持批量已读/星标/归档/删除
- ↩️ **可撤销**：归档、删除、清空垃圾箱后 5 秒内可在 Toast 里一键「撤销」
- 📏 **列表密度三档**：舒适 / 紧凑 / 单行，分段控件或命令面板切换，随偏好持久化
- ✉️ **完整写信流**：新邮件 / 回复 / 回复全部 / 转发，草稿自动保存，Ctrl+Enter 发送；从草稿发送后草稿自动消失
- 🔍 **全文搜索**：主题、发件人、正文跨账户检索
- 🌗 **亮 / 暗双主题**：暖纸亮色 + 暖炭暗色，跟随系统偏好，随时切换（`T`）
- 💾 **本地持久化**：桌面端邮件、已读/星标状态、AI 分析结果和邮箱配置保存在应用数据目录的 SQLite 文件中；浏览器开发模式使用 localStorage 演示
- 🔒 **本地隐私**：不提供应用账号体系，不依赖远程同步服务；邮箱授权码以 AES-256-GCM 加密后保存在本机

## 键盘快捷键

| 按键 | 动作 | 按键 | 动作 |
| --- | --- | --- | --- |
| `J` / `K` | 下一封 / 上一封 | `E` | 归档（可撤销） |
| `S` | 星标 | `R` / `A` / `F` | 回复 / 回复全部 / 转发 |
| `C` | 写新邮件 | `#` / `Delete` | 移入垃圾箱（可撤销） |
| `/` | 聚焦搜索 | `1`–`6` | 切换文件夹 |
| `T` | 切换深浅主题 | `Ctrl+K` | 命令面板 |
| `Ctrl+A` | 全选当前列表 | `Esc` | 清除选择 / 关闭弹层 / 存草稿 |
| Ctrl+点击 | 勾选/取消勾选 | Shift+点击勾选 | 范围勾选 |
| `Esc` | 关闭弹层 / 存草稿 | `Ctrl+Enter` | 发送 |

## 快速开始

浏览器开发模式前置：Node 20+、pnpm。桌面端构建还需要 Rust 工具链（见下文）。

**1. 安装依赖**

```bash
pnpm install
pnpm dev        # 前端开发服务器，通常为 http://localhost:5173
```

打开页面即可进入演示收件箱。浏览器模式不连接真实邮箱；真实 IMAP/SMTP 功能需要运行桌面版。

## 桌面应用（Windows / macOS / Linux）

桌面壳基于 Tauri 2，需要一次性安装 Rust 工具链。

**1. 安装前置**（仅首次，Windows 下可用 choco 一键完成）

| 平台 | 需要安装 |
| --- | --- |
| Windows | `choco install rustup.install visualstudio2022buildtools -y --package-parameters "--add Microsoft.VisualStudio.Workload.VCTools --includeRecommended"`（WebView2 运行时 Win10/11 已内置） |
| macOS | `xcode-select --install` + [rustup](https://rustup.rs/) |
| Linux | `sudo apt install libwebkit2gtk-4.1-dev build-essential curl wget file libxdo-dev libssl-dev libayatana-appindicator3-dev librsvg2-dev` + [rustup](https://rustup.rs/) |

**2. 开发 / 构建**

```bash
pnpm tauri dev    # 桌面窗口开发模式（热更新）
pnpm tauri build  # 产出各平台安装包（msi/dmg/deb/AppImage…）
```

安装包输出在 `src-tauri/target/release/bundle/`。

> 桌面版使用应用数据目录中的单文件 SQLite 数据库 `vela.db`，启动后自动创建，无需注册、登录或启动额外服务。删除该文件会清空本地邮件和邮箱配置，请先备份。

> **Windows 构建已实测通过**（Rust 1.98 + MSVC 14.44 + Win SDK 10.0.26100）：产出 `Vela_0.2.0_x64_en-US.msi`、`Vela_0.2.0_x64-setup.exe`（NSIS）与便携版 `vela.exe`，窗口启动、WebView2 渲染与本地数据读写验证正常。

> 如需重新生成全套图标：`pnpm tauri icon src-tauri/icons/icon.png`

## 项目结构

```
vela-mail/
├── src/
│   ├── components/      # Sidebar / MailList / MailViewer / ComposeWindow / CommandPalette / Toasts
│   ├── store/           # MailContext：本地会话、业务动作和持久化
│   ├── services/        # 本地数据库桥接、邮箱协议、AI 分析和本地分类
│   ├── data/            # 浏览器演示数据：3 个演示账户和示例邮件
│   ├── hooks/           # useHotkeys 全局快捷键
│   └── styles/          # 设计令牌 + 全局样式（纸墨美学）
├── scripts/             # Android 构建准备和 IMAP/SMTP 模拟服务器
├── src-tauri/           # Tauri 2 桌面壳（Rust）
│   ├── src/localdb.rs   # 本地 SQLite 数据库
│   └── src/mail/        # IMAP/SMTP、授权码加密和邮箱同步
└── index.html
```

## 本地数据

桌面端启动时会在应用数据目录创建 `vela.db`，使用 SQLite 保存：

- **`emails`**：邮件正文、文件夹、已读/星标状态、分类和 AI 摘要
- **`mailbox_accounts`**：已接入的真实邮箱、同步游标和加密后的授权码
- 前端状态变化后会防抖写入本地数据库，不依赖网络或远程账号
- 浏览器开发模式没有 Tauri 本地数据库，会回退到演示数据和 localStorage

## 接入真实邮箱

桌面版可以直接使用真实的 QQ / 163 / Gmail / Outlook 等邮箱（IMAP 收信 + SMTP 发信）。

**为什么只能在桌面版用**：浏览器出于安全限制无法直接建立 TCP 连接，而 IMAP/SMTP 都是 TCP 协议，
因此需要 Tauri 的原生能力。浏览器里打开会明确提示改用桌面版。

**使用步骤**

1. `pnpm tauri dev` 启动桌面版（或安装 `pnpm tauri build` 产出的安装包）
2. 侧栏底部点「**+ 接入真实邮箱**」
3. 选服务商（按邮箱域名会自动匹配）→ 填邮箱地址与**授权码** → 先「测试连接」再「保存并接入」
4. 点侧栏「同步」按钮收取邮件；真实账户名后有个小圆点标记

**关于授权码**：这里必须填邮箱服务商给的「授权码 / 应用专用密码」，
**不是网页登录密码**。各服务商开启方式不同，向导里会显示对应提示：

| 服务商 | 服务器 | 授权码获取 |
| --- | --- | --- |
| QQ 邮箱 | imap.qq.com:993 / smtp.qq.com:465 | 设置 → 账户 → 开启 IMAP/SMTP → 生成 16 位授权码 |
| 163 / 126 | imap.163.com:993 / smtp.163.com:465 | 设置 → POP3/SMTP/IMAP → 开启后生成授权码 |
| Gmail | imap.gmail.com:993 / smtp.gmail.com:465 | 开启两步验证后生成「应用专用密码」（国内需能访问 Google） |
| Outlook | outlook.office365.com:993 / smtp.office365.com:587 | 账户密码；开了双重验证则需应用密码 |
| iCloud | imap.mail.me.com:993 / smtp.mail.me.com:587 | Apple ID 设置里生成 App 专用密码 |

自建 / 企业邮箱选「自定义」手动填服务器地址。

**支持的能力**

- 拉取收件箱与已发送（**增量同步**：用 UID 游标只取新邮件，UIDVALIDITY 变化时自动全量重来）
- **历史邮件分批回填**：首次接入优先显示**最近的**邮件，更早的邮件不会丢——每次同步继续向前回填一批，
  提示栏会告诉你「再点一次同步可加载更早的邮件」，直到全部拉完（详见下文）
- SMTP 真实投递（发件后服务器通常会自动归档到已发送，无需客户端重复写入）
- 已读 / 星标**回写到邮箱服务器**（换个客户端登录也能看到一致状态）
- 归档 / 删除会真实移动到服务器的对应文件夹（优先用 MOVE 扩展）
- 中文邮件正确解码（含 QQ/163 常见的 GBK 编码）
- 附件只同步元信息（名称 / 大小 / 类型），不下载正文避免占用

## 历史邮件是怎么拉回来的

一个用了多年的邮箱可能有上万封邮件，不可能在一次同步里全下完（会卡很久）。
因此这里用了**两个方向的游标**，各管一件事：

```
        UID 1 ──────────────────────────────────► UID N（最新）
             ▲                                  ▲
             │                                  │
      backfill_uid                       last_uid
   （向更旧方向逐批回填）            （向更新方向增量拉取）
```

- **`last_uid`**：已同步到的最新 UID，只管新到的邮件
- **`backfill_uid`**：历史邮件回填进度，每次继续往前一批

单次同步每个文件夹最多 300 封（可用 `MAX_FETCH_PER_SYNC` 调整）。首次接入时：

1. 先取**最新的一批**（让你马上看到近期邮件，而不是几年前的）
2. 剩余的老邮件记录为「待回填」
3. 之后每次点同步，继续向前回填 300 封，直到 `backfill_uid` 归零
4. 同步提示会告诉你还有更早的邮件可加载

> 早期实现只有一个游标，首次接入截断后游标直接跳到末尾，被丢掉的老邮件就**永远拉不到**了。
> 现在有专门的回归测试锁定这个行为（见 `src-tauri/tests/backfill_e2e.rs`，用 1000 封邮件的模拟邮箱
> 验证「不重不漏」）。

**凭证安全**

授权码用 **AES-256-GCM** 加密后存入本地 SQLite 的 `mailbox_accounts` 表，数据库里只有密文，
前端永远拿不到明文。密钥来源优先级：

1. 环境变量 `VELA_SECRET`（可选）
2. 应用数据目录下的 `mail.key`（不存在时首次运行自动生成随机密钥）

> 换了密钥后旧密文无法解密，需要重新输入授权码。

## 架构说明

```
src-tauri/src/
├── mail/                  # 真实邮箱接入层
│   ├── providers.rs       # 服务商预设（服务器地址 / 授权码提示）
│   ├── crypto.rs          # AES-256-GCM 凭证加密
│   ├── store.rs           # 邮箱账户与同步游标的 SQLite 存取
│   ├── imap.rs            # IMAP 收信 + 状态回写（已读/星标/归档/删除）+ IDLE 实时监控
│   ├── watcher.rs         # 后台 IDLE 监控：新邮件实时推送（vela://new-mail 事件）
│   ├── smtp.rs            # SMTP 发信
│   ├── types.rs           # 与前端的数据契约
│   └── commands.rs        # 暴露给前端的 Tauri command
└── localdb.rs             # 本地邮件与邮箱账户数据库
```

**实时通知链路**：桌面端启动即为每个已接入邮箱建立 IDLE 长连接（服务器不支持
IDLE 时自动退化为每分钟轮询）；服务器一推送就做一次仅增量拉取，通过
`vela://new-mail` 事件推给前端合并状态并弹系统通知。断线按指数退避重连
（15 秒起、上限 5 分钟）。关窗只是最小化到系统托盘，监控继续；托盘菜单提供
「显示主窗口 / 立即同步 / 退出」。

前端在两种环境自动选择后端：

| 环境 | 数据与持久化 | 真实邮箱 |
| --- | --- | --- |
| 桌面（Tauri） | 本地 SQLite（应用数据目录） | Rust command（完整可用） |
| 浏览器（`pnpm dev`） | 演示数据 + localStorage | 不可用（明确提示） |

## 测试

```bash
# Rust 单元测试
cd src-tauri && cargo test

# 端到端测试需要模拟 IMAP/SMTP 服务器（普通邮箱 + 大邮箱各一个）：
python scripts/mock_mail_server.py 11143 11025 &
MOCK_BULK=1 python scripts/mock_mail_server.py 11243 11026 &
cd src-tauri && cargo test
```

集成测试覆盖：IMAP 认证、增量拉取、**历史邮件分批回填（1000 封不重不漏）**、GBK 中文解码、
已读/星标回写、文件夹移动、EXAMINE 切换选中邮箱的回归场景。

模拟服务器支持 `MOCK_BULK=1` 环境变量生成一个大邮箱（默认 1000 封），用于验证回填逻辑；`MOCK_BULK_COUNT` 可改邮件数量。

## 后续路线

- 草稿同步到服务器（目前草稿仅本地保存）
- 附件下载 / 上传（当前只同步附件元信息）
- OAuth2 认证（Gmail / Outlook 免授权码，体验更好）
- 分类引擎接入 LLM API（`classifyService.ts` 接口不变）

## 技术栈

React 19 · TypeScript 7 · Vite 8 · Tauri 2 · Rust（imap / lettre / mail-parser / aes-gcm / rusqlite）· SQLite · lucide-react · Fontsource 自托管字体（Bricolage Grotesque / Outfit / Newsreader）
