# ops-panel

一个**轻量、安全优先**的运维小面板，配合 1panel 使用。1panel 管基础设施，ops-panel 做自定义功能。

## 一键启动

前置：Go ≥ 1.22、Node ≥ 20、pnpm ≥ 9。首次运行会自动 `pnpm install` + 编译后端。

### 开发模式

```bash
# Linux / macOS / Windows(git-bash)
./dev.sh

# Windows PowerShell
.\dev.ps1
```

启动后：
- 前端：<http://localhost:5173>
- 后端 API：<http://127.0.0.1:8443/api>

**首次启动**会打印 admin 初始密码 + TOTP 种子到终端，也写到 `~/.ops-panel/FIRST_RUN_CREDENTIALS.txt`：

1. 把 TOTP 种子扫进 Authenticator (Google Authenticator / Authy / 1Password / Aegis)
2. 复制初始密码，登录
3. 首次登录强制修改密码
4. **删除** `FIRST_RUN_CREDENTIALS.txt`

`Ctrl+C` 同时停止前端和后端。

### 生产构建

```bash
./build.sh                            # 默认 linux/amd64
GOOS=linux  GOARCH=arm64 ./build.sh   # ARM 服务器
GOOS=darwin GOARCH=arm64 ./build.sh   # macOS (M 系列)

# Windows PowerShell:
$env:GOOS="linux"; $env:GOARCH="arm64"; .\build.ps1
```

产物在 `dist/`：静态编译的 `ops-panel` 二进制（~16MB, 无 cgo, 单文件）+ `frontend/` 静态文件 + `scripts/` + `ops-panel.service`。上传到服务器按脚本末尾的提示即可。

## 为什么

1panel 付费功能（告警、审计、WAF 等）其实很多都不难自己实现，但面板的**攻击面必须尽量小**。本项目的原则：

- **默认只监听 `127.0.0.1`**。外网访问强制走 Tailscale / WireGuard / Cloudflare Tunnel
- **必须 TOTP 双因素**，不可关闭
- 密码 argon2id、所有 state-changing 请求 CSRF 校验
- 登录防爆破：IP 5次失败锁 15 分钟、账户 10次失败锁 1 小时、IP 白名单（可选）
- 审计日志 append-only + SHA-256 hash chain（被人清掉一条就断链）
- 纯 SPA（Vite + React），不用 Next.js（避开 SSR/中间件类 CVE）
- 后端零 C 依赖（`modernc.org/sqlite`，静态二进制）

## 架构

```
  浏览器 ── HTTPS ──▶  Caddy/Nginx  ──▶  ops-panel (Go)
                      │ (TLS 终结)         │
                      │                    ├── SQLite (~/.ops-panel/panel.db)
                      └── 1panel ─────     └── gopsutil (系统指标)
                          (9999 等)

建议：1panel 和 ops-panel 都绑到 127.0.0.1，Caddy/Nginx 做唯一的公开入口，
走 Cloudflare Tunnel 或 WireGuard 连上服务器再访问。
```

## 项目结构

```
ops-panel/
├── backend/                    Go 后端
│   ├── cmd/panel/main.go
│   └── internal/
│       ├── auth/              argon2id + TOTP + JWT + rate limit
│       ├── api/               HTTP handlers
│       ├── config/            配置文件 + 首次启动
│       ├── middleware/        安全头、CSRF、IP 白名单、Auth
│       ├── storage/           SQLite schema + 所有查询
│       └── system/            gopsutil 封装
├── frontend/                  React + Vite + TS + Tailwind
│   └── src/
│       ├── pages/             Login / Dashboard / ChangePassword / Audit
│       ├── components/
│       ├── api.ts             fetch 包装（CSRF + token 管理）
│       └── auth.tsx
└── scripts/
    ├── ssh-harden.sh          SSH 加固（key-only + fail2ban）
    ├── generate-cert.sh       自签 TLS（仅本机用）
    └── ops-panel.service      systemd 单元（带沙箱）
```

## 手动启动（调试用）

`./dev.sh` / `./dev.ps1` 底下就是这两步：

```bash
# 后端
cd backend && go run ./cmd/panel -listen 127.0.0.1:8443

# 前端（另开一个终端）
cd frontend && pnpm install && pnpm dev
```

Vite 会把 `/api/*` 代理到后端 `127.0.0.1:8443`。打开 <http://localhost:5173>。

## 生产部署

### 编译

一键：`./build.sh` 或 `./build.ps1`（见上方[一键启动](#一键启动)）。

手动：

```bash
cd backend
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o ops-panel ./cmd/panel

cd ../frontend
pnpm install --frozen-lockfile
pnpm build         # 产物在 frontend/dist
```

### 安装

```bash
# 在服务器上
useradd -r -m -d /var/lib/ops-panel -s /usr/sbin/nologin opspanel
install -m 0755 ops-panel /usr/local/bin/
mkdir -p /var/lib/ops-panel/frontend
cp -r frontend/dist/* /var/lib/ops-panel/frontend/
chown -R opspanel:opspanel /var/lib/ops-panel

install -m 0644 scripts/ops-panel.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now ops-panel
journalctl -u ops-panel -f     # 看首次启动的凭据
```

### 反向代理（Caddy 例子）

```caddyfile
panel.example.com {
    encode gzip
    reverse_proxy 127.0.0.1:8443 {
        header_up X-Real-IP {remote_host}
    }
}
```

用 Caddy 的话 ops-panel 可以不配 TLS（后端内网明文），前提是**加 `-trust-proxy` 参数**让它读 `X-Real-IP` / `X-Forwarded-For`，否则所有请求看起来都从 `127.0.0.1` 来，rate limit 会失效。

### 外网访问强烈建议走 Tailscale

```bash
# 服务器
curl -fsSL https://tailscale.com/install.sh | sh
tailscale up

# 客户端（你的笔记本）装 Tailscale 后访问：
http://<server-tailscale-ip>:8443
```

这样 ops-panel 根本不需要公网暴露，`ListenAddr` 可以一直是 `127.0.0.1:8443`。

## 服务器管理 CLI (`opsctl`)

`install.sh` 会顺手装一个 `opsctl` 到 `/usr/local/bin/`，在服务器上登录 root / sudo 用：

```bash
opsctl status           # 服务状态 + URL + 用户列表 + admin 是否绑定 TOTP
opsctl restart          # 重启
opsctl logs -f          # 跟日志（等价于 journalctl -u ops-panel -f）
opsctl passwd [user]    # 交互式重置密码（默认 user=admin）
opsctl reset-2fa [user] # 解绑 Authenticator（手机丢了用这个，之后登录只需密码）
opsctl info             # JSON 输出：配置 + 用户列表，给脚本吃
opsctl cleanup-cred     # 删除 FIRST_RUN_CREDENTIALS.txt
opsctl uninstall        # 卸载（大红字二次确认，可选是否一起删数据目录和服务用户）
opsctl help             # 完整命令列表
```

底层是 systemd + 主二进制的 `ops-panel admin <subcommand>`（需要 root，因为 DB 归 `opspanel` 用户）。脚本化场景也可以直接调 `sudo -u opspanel /usr/local/bin/ops-panel admin ...`。

## 配置文件

`~/.ops-panel/config.json`（或 `-config` 指定）：

```json
{
  "listen_addr": "127.0.0.1:8443",
  "tls_cert_file": "",
  "tls_key_file": "",
  "data_dir": "/var/lib/ops-panel",
  "jwt_secret": "...自动生成...",
  "issuer": "ops-panel",
  "allowed_ips": ["100.64.0.0/10", "10.0.0.0/8"]
}
```

- `allowed_ips` 留空 = 不限制
- 写了就是 IP 白名单，其他 IP 直接 403（记得把你的 Tailscale 网段填进去）

## 安全基线自检

| 项 | 内置 | 说明 |
|---|---|---|
| argon2id 密码哈希 | ✅ | m=64MB, t=3, p=2 |
| TOTP 双因素 | ✅ | SHA1 30s 6位，与主流 app 兼容 |
| 防爆破 (IP) | ✅ | 5次失败 15 分钟 |
| 防爆破 (账号) | ✅ | 10次失败 1 小时 |
| IP 白名单 | ✅ | `allowed_ips` 配置 |
| CSRF double-submit | ✅ | `panel_csrf` cookie + `X-CSRF-Token` |
| CSP / HSTS / X-Frame-Options | ✅ | 严格 CSP，没 inline script |
| 审计 hash chain | ✅ | SHA-256 链式 |
| Session 可强制下线 | ✅ | 服务端 sessions 表 |
| 首次强制改密 | ✅ | `must_change_password` flag |
| systemd 沙箱 | ✅ | 见 `scripts/ops-panel.service` |

## 当前未做（等你说要再做）

- WebSocket 终端 / 远程执行
- 文件管理器
- Docker 容器管理
- 邮件/钉钉/飞书告警
- 和 1panel API 对接

等你说要哪个再做。

## 开发约定

- 不加多余依赖。每多一个依赖就多一个供应链风险
- 新路由默认挂在 auth + CSRF 后面。只有在明确需要公开时才绕过（且必须写注释说明为什么）
- 任何高危操作（改密、删除、重启服务等）都要：过 TOTP 二次确认 + 写 audit log
- HTTP handler 不直接访问 DB，经过 storage 层
- 错误消息对外**不要**包含详细栈；日志里详细打

## 许可

MIT
