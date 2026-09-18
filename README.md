# Vela 轻帆 ⛵

**现代、美观、快速的跨平台邮件客户端** —— Tauri 2 + React 19 + TypeScript。

第一版以演示数据驱动，专注界面与交互打磨：编辑风「纸墨」美学（暖纸底色、墨色文字、朱砂点缀），邮件正文用衬线字体呈现，还原"读信"的仪式感。

## 功能特性

- 📬 **三栏工作台**：账户/文件夹侧栏 · 邮件列表 · 沉浸阅读窗格
- 🧠 **AI 智能分类与摘要**：重要/财务/出行/社交/订阅/通知/垃圾七类自动归类，每封邮件一句话摘要，重要邮件附「为什么重要」；本地规则引擎离线运行，**发信即时分类**，未分类邮件可在阅读窗格一键「本地规则分析」
- 👥 **多账户管理**：3 个演示账户（Gmail / Outlook / QQ）+ 可接入真实邮箱，统一收件箱与单账户切换
- 📡 **真实邮箱接入**：桌面版支持 IMAP 收信 / SMTP 发信，已读星标归档删除均回写服务器（详见下文）
- ⌨️ **键盘优先**：Gmail / Superhuman 风格快捷键 + `Ctrl+K` 命令面板（搜操作、搜邮件、跳转一切）
- ✅ **多选与批量操作**：hover 圆圈勾选、Ctrl+点选、Shift 范围选、`Ctrl+A` 全选；浮出批量条支持批量已读/星标/归档/删除
- ↩️ **可撤销**：归档、删除、清空垃圾箱后 5 秒内可在 Toast 里一键「撤销」
- 📏 **列表密度三档**：舒适 / 紧凑 / 单行，分段控件或命令面板切换，随偏好持久化
- ✉️ **完整写信流**：新邮件 / 回复 / 回复全部 / 转发，草稿自动保存，Ctrl+Enter 发送；从草稿发送后草稿自动消失
- 🔍 **全文搜索**：主题、发件人、正文跨账户检索
- 🌗 **亮 / 暗双主题**：暖纸亮色 + 暖炭暗色，跟随系统偏好，随时切换（`T`）
- 👤 **账户体系**：注册 / 登录 / 会话保持（bcrypt 加盐 + Bearer 令牌），每用户邮箱数据在 PostgreSQL 中完全隔离；也可一键进入「游客模式」纯本地使用
- 💾 **双通道持久化**：登录用户数据落 PostgreSQL（防抖全量同步、崩溃/刷新可恢复）；游客模式写 localStorage；主题与列表密度偏好始终本地保存

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

前置：Node 20+、pnpm、PostgreSQL（仅登录功能需要，游客模式不需要）。

**1. 安装依赖**

```bash
pnpm install
cd server && pnpm install && cd ..
```

**2. 配置数据库**（没有 PG 可跳过，直接用游客模式）

复制 `server/.env.example` 为 `server/.env` 并填入你的连接串：

```ini
VELA_DB_URL=postgres://用户名:密码@localhost:5432/数据库名   # 密码中的 @ 需写成 %40
VELA_PORT=3210
```

表结构（`users` / `sessions` / `emails`）首次启动时自动创建，无需手工迁移。

**3. 启动（两个终端）**

```bash
pnpm server     # 终端 1：同步服务 http://127.0.0.1:3210（注册/登录/邮件持久化）
pnpm dev        # 终端 2：前端 http://localhost:1420
```

打开页面 → 注册一个账户即可开始（新账户自动播种演示邮件）；或点「游客模式」纯本地体验。

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

> **桌面版自带同步服务**：应用启动时会在进程内拉起一套 Rust 实现的同款 API（`src-tauri/src/server.rs`，监听 `127.0.0.1:3210`），**无需再单独运行 `pnpm server`**，装上即用。数据库不可用时仅登录不可用，游客模式不受影响。

> 桌面版的数据库连接按此优先级解析：环境变量 `VELA_DB_URL` → 可执行文件同目录的 `vela.config.json` → 应用配置目录 `config.json` → 内置默认（本机开发库）。`vela.config.json` 支持两种写法：
> ```json
> { "dsn": "postgres://user:pass@localhost:5432/db" }
> ```
> ```json
> { "host": "localhost", "port": 5432, "user": "postgres", "password": "明文密码（无需转义）", "database": "postgres" }
> ```

> **浏览器开发模式**则使用 Node 版同步服务（`pnpm server`），两者接口完全一致、共用同一个数据库。

> **Windows 构建已实测通过**（Rust 1.98 + MSVC 14.44 + Win SDK 10.0.26100）：产出 `Vela_0.2.0_x64_en-US.msi`、`Vela_0.2.0_x64-setup.exe`（NSIS）与便携版 `vela.exe`，窗口启动、WebView2 渲染与内嵌服务接口验证正常。

> 如需重新生成全套图标：`pnpm tauri icon src-tauri/icons/icon.png`

## 项目结构

```
vela-mail/
├── src/
│   ├── components/      # LoginScreen / Sidebar / MailList / MailViewer / ComposeWindow / CommandPalette / Toasts
│   ├── store/           # MailContext：全局状态、认证会话、业务动作、双通道持久化
│   ├── services/        # api（同步服务客户端）/ mailService（过滤搜索格式化）/ classifyService（本地规则分类）
│   ├── data/            # 演示数据：3 账户 × 28 封真实感邮件（相对日期生成）
│   ├── hooks/           # useHotkeys 全局快捷键
│   └── styles/          # 设计令牌 + 全局样式（纸墨美学）
├── server/              # Node 同步服务（浏览器开发模式用；Express + pg + bcryptjs）
│   ├── index.mjs        # 注册/登录/会话/邮件同步 API，建表自动执行
│   └── .env             # 数据库连接配置（gitignore）
├── src-tauri/           # Tauri 2 桌面壳（Rust）
│   └── src/server.rs    # 内嵌同步服务（axum + tokio-postgres + bcrypt），桌面版随应用启动
└── index.html
```

## 数据模型

- **`users`**：账户（用户名唯一 + bcrypt 密码哈希）
- **`sessions`**：登录令牌（Bearer，登出即删）
- **`emails`**：每用户一份完整邮箱（`(user_id, id)` 主键，JSONB 存发件人/收件人/附件）
- **同步策略**：前端状态变更 800ms 防抖后全量 upsert，并删除服务端多余行；所有查询按 `user_id` 过滤，账户间互不可见
- **双实现**：`server/index.mjs`（Node，浏览器开发用）与 `src-tauri/src/server.rs`（Rust，桌面内嵌）实现同一套 API 与同一张表，前端无感知

## 接入真实邮箱

桌面版可以直接使用真实的 QQ / 163 / Gmail / Outlook 等邮箱（IMAP 收信 + SMTP 发信）。

**为什么只能在桌面版用**：浏览器出于安全限制无法直接建立 TCP 连接，而 IMAP/SMTP 都是 TCP 协议，
因此需要 Tauri 的原生能力。浏览器里打开会明确提示改用桌面版。

**使用步骤**

1. `pnpm tauri dev` 启动桌面版（或安装 `pnpm tauri build` 产出的安装包）
2. 注册 / 登录一个应用账户
3. 侧栏底部点「**+ 接入真实邮箱**」
4. 选服务商（按邮箱域名会自动匹配）→ 填邮箱地址与**授权码** → 先「测试连接」再「保存并接入」
5. 点侧栏「同步」按钮收取邮件；真实账户名后有个小圆点标记

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

授权码用 **AES-256-GCM** 加密后存入 PostgreSQL 的 `mailbox_accounts` 表，数据库里只有密文，
前端永远拿不到明文。密钥来源优先级：

1. 环境变量 `VELA_SECRET`（推荐，适合多设备统一管理）
2. 应用配置目录下的 `mail.key`（不存在时首次运行自动生成随机密钥）

> 换了密钥后旧密文无法解密，需要重新输入授权码。

## 架构说明

```
src-tauri/src/
├── mail/                  # 真实邮箱接入层
│   ├── providers.rs       # 服务商预设（服务器地址 / 授权码提示）
│   ├── crypto.rs          # AES-256-GCM 凭证加密
│   ├── store.rs           # 邮箱账户与同步游标的 PostgreSQL 存取
│   ├── imap.rs            # IMAP 收信 + 状态回写（已读/星标/归档/删除）
│   ├── smtp.rs            # SMTP 发信
│   ├── types.rs           # 与前端的数据契约
│   └── commands.rs        # 暴露给前端的 Tauri command
└── server.rs              # 内嵌同步服务：应用账户 + 邮件持久化
```

前端在两种环境自动选择后端：

| 环境 | 账户 / 持久化 | 真实邮箱 |
| --- | --- | --- |
| 桌面（Tauri） | Rust 内嵌服务 | Rust command（完整可用） |
| 浏览器（`pnpm dev`） | Node 服务 `server/` | 不可用（明确提示） |

## 测试

```bash
# Rust 单元测试 + 集成测试（共 58 个）
cd src-tauri && cargo test

# 端到端测试需要模拟 IMAP/SMTP 服务器（普通邮箱 + 大邮箱各一个）：
python scripts/mock_mail_server.py 11143 11025 &
MOCK_BULK=1 python scripts/mock_mail_server.py 11243 11026 &
cd src-tauri && cargo test
```

集成测试覆盖：登录鉴权、增量拉取、**历史邮件分批回填（1000 封不重不漏）**、GBK 中文解码、
已读/星标回写、文件夹移动、EXAMINE 切换选中邮箱的回归场景。

模拟服务器支持 `MOCK_BULK=1` 环境变量生成一个大邮箱（默认 1000 封），用于验证回填逻辑；`MOCK_BULK_COUNT` 可改邮件数量。

## 后续路线

- 草稿同步到服务器（目前草稿仅本地保存）
- 附件下载 / 上传（当前只同步附件元信息）
- OAuth2 登录（Gmail / Outlook 免授权码，体验更好）
- IMAP IDLE 实时推送（替代手动/定时同步）
- 分类引擎接入 LLM API（`classifyService.ts` 接口不变）

## 技术栈

React 19 · TypeScript 7 · Vite 8 · Tauri 2 · Rust（imap / lettre / mail-parser / aes-gcm）· axum + tokio-postgres · PostgreSQL · lucide-react · Fontsource 自托管字体（Bricolage Grotesque / Outfit / Newsreader）
