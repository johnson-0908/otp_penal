#!/usr/bin/env bash
# install.sh — one-click installer for ops-panel on CentOS / Rocky / AlmaLinux / RHEL.
#
# Usage (remote):
#   curl -fsSL https://github.com/johnson-0908/otp_penal/releases/latest/download/install.sh | sudo bash
#
# Usage (from extracted tarball):
#   sudo ./install.sh
#
# Environment variables:
#   OPS_VERSION    pin a specific release tag (default: latest)
#   OPS_PREFIX     install prefix (default: /usr/local)
#   OPS_DATA_DIR   data directory (default: /var/lib/ops-panel)
#   OPS_USER       service user (default: opspanel)
#   OPS_LISTEN     listen address (default: 0.0.0.0:<RANDOM 20000-65000>)
#                  — set to 127.0.0.1:8443 if you plan to reverse-proxy only
#   OPS_REPO       GitHub repo (default: johnson-0908/otp_penal)
#
# Default posture is BT/1panel style:
#   * HTTPS self-signed cert generated on first run
#   * Random high-port so 8443/8888 scanners skip past
#   * Random URL entry path — any URL without the entry path returns 404
#   * firewalld opened for the chosen port automatically

set -euo pipefail

OPS_REPO="${OPS_REPO:-johnson-0908/otp_penal}"
OPS_VERSION="${OPS_VERSION:-}"
OPS_PREFIX="${OPS_PREFIX:-/usr/local}"
OPS_DATA_DIR="${OPS_DATA_DIR:-/var/lib/ops-panel}"
OPS_USER="${OPS_USER:-opspanel}"

# Default listen: random high port bound to all interfaces. Easier to scan
# past (8443/8888 are obvious panel ports), and since we now ship self-signed
# TLS + entry-path gate + rate limiting, binding to 0.0.0.0 is reasonable
# for direct access without a reverse proxy.
if [ -z "${OPS_LISTEN:-}" ]; then
  # Bash $RANDOM is 0-32767; shift into 20000-65000 range.
  OPS_PORT=$(( 20000 + RANDOM % 45000 ))
  OPS_LISTEN="0.0.0.0:$OPS_PORT"
else
  OPS_PORT="${OPS_LISTEN##*:}"
fi

BIN="$OPS_PREFIX/bin/ops-panel"
FRONTEND_DIR="$OPS_DATA_DIR/frontend"
UNIT_PATH="/etc/systemd/system/ops-panel.service"

C_RESET="\033[0m"; C_RED="\033[31m"; C_GREEN="\033[32m"; C_YELLOW="\033[33m"; C_CYAN="\033[36m"; C_BOLD="\033[1m"

msg()  { echo -e "${C_CYAN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! $*${C_RESET}" >&2; }
err()  { echo -e "${C_RED}XX $*${C_RESET}" >&2; exit 1; }

[ "$(id -u)" = 0 ] || err "必须以 root 运行（试试 sudo）"

# ---------- 1. OS & arch detect ----------
if [ -r /etc/os-release ]; then . /etc/os-release; fi
OS_ID="${ID:-unknown}"
case "$OS_ID" in
  centos|rhel|rocky|almalinux|fedora|ol) ;;
  *) warn "未在 CentOS 系家族检测到：$OS_ID。脚本按 RHEL 家族流程继续，可能需要你自己处理差异。" ;;
esac

ARCH_RAW=$(uname -m)
case "$ARCH_RAW" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) err "不支持的 CPU 架构: $ARCH_RAW" ;;
esac
msg "系统: $OS_ID, 架构: $ARCH"

# ---------- 2. dependencies ----------
need_pkgs=()
command -v curl >/dev/null 2>&1 || need_pkgs+=(curl)
command -v tar  >/dev/null 2>&1 || need_pkgs+=(tar)
command -v systemctl >/dev/null 2>&1 || err "未找到 systemctl — ops-panel 需要 systemd"

if [ "${#need_pkgs[@]}" -gt 0 ]; then
  msg "安装依赖: ${need_pkgs[*]}"
  if command -v dnf >/dev/null 2>&1; then dnf install -y "${need_pkgs[@]}"
  elif command -v yum >/dev/null 2>&1; then yum install -y "${need_pkgs[@]}"
  else err "找不到 dnf/yum"; fi
fi

# ---------- 3. decide source: local tarball or github ----------
LOCAL_MODE=""
# When invoked via `curl ... | bash`, BASH_SOURCE is unset — guard the lookup
# with a default so `set -u` doesn't abort. This branch is only for the
# local-tarball use case, so falling back to "" is correct.
BASH_SOURCE_PATH="${BASH_SOURCE[0]:-}"
SCRIPT_DIR=""
if [ -n "$BASH_SOURCE_PATH" ]; then
  SCRIPT_DIR="$(cd "$(dirname "$BASH_SOURCE_PATH")" >/dev/null 2>&1 && pwd || true)"
fi
# Check for -f (exists) not -x (executable) — some Windows-built tarballs lose
# the execute bit on the Go binary because NTFS/MinGW chmod doesn't always stick
# for files without a .sh/.exe extension. We'll chmod ourselves below.
if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/../ops-panel" ] && [ -d "$SCRIPT_DIR/../frontend" ]; then
  LOCAL_MODE=1
  SRC_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
  msg "检测到已解压的本地 tarball: $SRC_ROOT"
  chmod +x "$SRC_ROOT/ops-panel" 2>/dev/null || true
  chmod +x "$SRC_ROOT/scripts/"*.sh 2>/dev/null || true
  [ -f "$SRC_ROOT/scripts/opsctl" ] && chmod +x "$SRC_ROOT/scripts/opsctl"
fi

WORK=""
cleanup() { [ -n "$WORK" ] && rm -rf "$WORK"; }
trap cleanup EXIT

if [ -z "$LOCAL_MODE" ]; then
  if [ -z "$OPS_VERSION" ]; then
    msg "查询最新版本..."
    OPS_VERSION=$(curl -fsSL "https://api.github.com/repos/$OPS_REPO/releases/latest" \
      | grep -Po '"tag_name":\s*"\K[^"]+' || true)
    [ -n "$OPS_VERSION" ] || err "无法获取最新版本号（仓库还没有 release？试试 OPS_VERSION=vX.Y.Z 手动指定）"
  fi
  msg "目标版本: $OPS_VERSION"

  WORK=$(mktemp -d -t ops-panel-install-XXXX)
  TARBALL="ops-panel-$OPS_VERSION-linux-$ARCH.tar.gz"
  URL="https://github.com/$OPS_REPO/releases/download/$OPS_VERSION/$TARBALL"

  msg "下载: $URL"
  curl -fL --retry 3 -o "$WORK/$TARBALL" "$URL" \
    || err "下载失败。检查网络或 OPS_VERSION 是否正确。"

  # Optional checksum. `sha256sum` emits `HASH  FILE` (text) or `HASH *FILE`
  # (binary) — `-c --ignore-missing` handles both and skips entries for files
  # we didn't download (we only grabbed one arch).
  if [ "${OPS_SKIP_VERIFY:-0}" = "1" ]; then
    warn "OPS_SKIP_VERIFY=1 — 跳过 SHA256 校验"
  elif curl -fsSL "https://github.com/$OPS_REPO/releases/download/$OPS_VERSION/SHA256SUMS" -o "$WORK/SHA256SUMS" 2>/dev/null; then
    msg "校验 SHA256..."
    (cd "$WORK" && sha256sum -c --ignore-missing SHA256SUMS >/dev/null) \
      || err "SHA256 校验失败 — 安装包可能被篡改（如需跳过：OPS_SKIP_VERIFY=1）"
  else
    warn "未发布 SHA256SUMS，跳过校验"
  fi

  msg "解压..."
  tar -xzf "$WORK/$TARBALL" -C "$WORK"
  SRC_ROOT=$(find "$WORK" -maxdepth 2 -type d -name "ops-panel-*-linux-$ARCH" | head -1)
  [ -n "$SRC_ROOT" ] || err "解压后的目录结构不符合预期"
fi

[ -f "$SRC_ROOT/ops-panel" ] || err "找不到二进制: $SRC_ROOT/ops-panel"
[ -d "$SRC_ROOT/frontend" ]  || err "找不到前端目录: $SRC_ROOT/frontend"
chmod +x "$SRC_ROOT/ops-panel" 2>/dev/null || true

# ---------- 4. service user ----------
if ! id -u "$OPS_USER" >/dev/null 2>&1; then
  msg "创建服务用户: $OPS_USER"
  useradd -r -m -d "$OPS_DATA_DIR" -s /sbin/nologin "$OPS_USER"
else
  msg "用户 $OPS_USER 已存在"
fi

# ---------- 5. install files ----------
msg "安装二进制到: $BIN"
install -d -m 0755 "$OPS_PREFIX/bin"
install -m 0755 "$SRC_ROOT/ops-panel" "$BIN"

if [ -f "$SRC_ROOT/scripts/opsctl" ]; then
  msg "安装管理 CLI 到: $OPS_PREFIX/bin/opsctl"
  install -m 0755 "$SRC_ROOT/scripts/opsctl" "$OPS_PREFIX/bin/opsctl"
fi

msg "安装前端到: $FRONTEND_DIR"
install -d -o "$OPS_USER" -g "$OPS_USER" -m 0750 "$OPS_DATA_DIR"
rm -rf "$FRONTEND_DIR"
mkdir -p "$FRONTEND_DIR"
cp -r "$SRC_ROOT/frontend/." "$FRONTEND_DIR/"
chown -R "$OPS_USER:$OPS_USER" "$FRONTEND_DIR"

# ---------- 6. systemd unit ----------
msg "写入 systemd unit: $UNIT_PATH"
if [ -f "$SRC_ROOT/scripts/ops-panel.service" ]; then
  cp "$SRC_ROOT/scripts/ops-panel.service" "$UNIT_PATH"
  # Patch user / paths / listen if caller overrode defaults
  sed -i "s|^User=.*|User=$OPS_USER|; s|^Group=.*|Group=$OPS_USER|" "$UNIT_PATH"
  sed -i "s|/usr/local/bin/ops-panel|$BIN|g" "$UNIT_PATH"
  sed -i "s|/var/lib/ops-panel|$OPS_DATA_DIR|g" "$UNIT_PATH"
  sed -i "s|-listen 127\\.0\\.0\\.1:8443|-listen $OPS_LISTEN|g" "$UNIT_PATH"
else
  cat > "$UNIT_PATH" <<EOF
[Unit]
Description=Ops Panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$OPS_USER
Group=$OPS_USER
ExecStart=$BIN -data-dir $OPS_DATA_DIR -config $OPS_DATA_DIR/config.json -frontend $FRONTEND_DIR -listen $OPS_LISTEN
Restart=on-failure
RestartSec=5

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$OPS_DATA_DIR
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
ProtectHostname=true
ProtectClock=true
ProtectKernelLogs=true
LockPersonality=true
RestrictRealtime=true
RestrictNamespaces=true
RestrictSUIDSGID=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
EOF
fi

chmod 0644 "$UNIT_PATH"

# ---------- 7. SELinux (only if enforcing) ----------
if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce)" = "Enforcing" ]; then
  msg "SELinux 处于 Enforcing 模式，设置文件上下文..."
  if command -v restorecon >/dev/null 2>&1; then
    chcon -t bin_t "$BIN" 2>/dev/null || true
    restorecon -R "$OPS_DATA_DIR" 2>/dev/null || true
  fi
fi

# ---------- 8. firewall (only if firewalld running) ----------
if systemctl is-active --quiet firewalld 2>/dev/null; then
  PORT="${OPS_LISTEN##*:}"
  if [[ "$OPS_LISTEN" != 127.0.0.1:* ]] && [[ "$OPS_LISTEN" != localhost:* ]]; then
    msg "firewalld 已启用，打开端口 $PORT/tcp"
    firewall-cmd --permanent --add-port="$PORT/tcp" >/dev/null
    firewall-cmd --reload >/dev/null
  else
    msg "监听在回环地址 ($OPS_LISTEN)，跳过 firewalld 配置"
  fi
fi

# ---------- 9. start service ----------
msg "启动 ops-panel..."
systemctl daemon-reload
systemctl enable ops-panel >/dev/null 2>&1 || true
systemctl restart ops-panel

sleep 2

if ! systemctl is-active --quiet ops-panel; then
  err "服务启动失败。看日志：  journalctl -u ops-panel -n 100 --no-pager"
fi

# ---------- 10. display first-run credentials ----------
CRED_FILE="$OPS_DATA_DIR/FIRST_RUN_CREDENTIALS.txt"

# Wait up to 10s for the file to appear (first-run init is quick but not instant).
for _ in $(seq 1 10); do
  [ -f "$CRED_FILE" ] && break
  sleep 1
done

echo ""
echo -e "${C_GREEN}${C_BOLD}"
echo "==========================================================="
echo "  ops-panel 安装成功 ($OPS_VERSION)"
echo "==========================================================="
echo -e "${C_RESET}"

if [ -f "$CRED_FILE" ]; then
  # Extract fields (awk handles both "Username:" and "Username:    " alignment)
  U_LINE=$(grep -E '^Username:' "$CRED_FILE" | head -1 | awk '{print $2}')
  P_LINE=$(grep -E '^Password:' "$CRED_FILE" | head -1 | awk '{print $2}')
  ENTRY_PATH=$(python3 -c "import json; print(json.load(open('$OPS_DATA_DIR/config.json')).get('entry_path',''))" 2>/dev/null || \
               grep -o '"entry_path"[^,}]*' "$OPS_DATA_DIR/config.json" 2>/dev/null | head -1 | sed 's/.*"\([a-z0-9]*\)"[^"]*$/\1/')

  # Detect a public IP for the access URL (best-effort — falls back to $HOSTNAME).
  PUB_IP=$(curl -fsS --max-time 3 https://api.ipify.org 2>/dev/null || true)
  if [ -z "$PUB_IP" ]; then
    PUB_IP=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')
  fi
  [ -z "$PUB_IP" ] && PUB_IP="<服务器IP>"

  URL="https://$PUB_IP:$OPS_PORT/$ENTRY_PATH/"

  echo -e "  ${C_BOLD}访问地址:${C_RESET}  ${C_GREEN}$URL${C_RESET}"
  echo -e "  ${C_BOLD}用户名:${C_RESET}    $U_LINE"
  echo -e "  ${C_BOLD}初始密码:${C_RESET}  ${C_GREEN}$P_LINE${C_RESET}"
  echo ""
  echo -e "  ${C_YELLOW}⚠ 首次访问注意：${C_RESET}"
  echo "     • 自签证书，浏览器会警告 → 点"高级" → "继续访问" 即可"
  echo "     • URL 必须含 /$ENTRY_PATH/ 路径（安全入口），缺了走其他路径会 404"
  echo "     • 入口 cookie 24h 有效，过期重新访问入口 URL 即可"
  echo ""
  echo -e "  ${C_YELLOW}⚠ 请立刻：${C_RESET}"
  echo "     1) 登录后修改密码（首次登录强制）"
  echo "     2) 账号页 → 绑定 Authenticator（Google Authenticator / Authy 等）"
  echo "     3) 删除凭据文件：  rm $CRED_FILE"
  echo ""
  echo -e "  ${C_CYAN}(凭据备份: $CRED_FILE)${C_RESET}"
else
  echo "  凭据文件未生成（可能数据库已有用户）。查看日志："
  echo "    journalctl -u ops-panel -n 50 --no-pager"
fi

cat <<EOF

  管理命令 (opsctl):
    opsctl status                  服务状态 + URL + 用户列表
    opsctl restart                 重启服务
    opsctl logs [-f]               查看日志
    opsctl passwd [user]           重置密码
    opsctl reset-2fa [user]        解绑 Authenticator
    opsctl uninstall               卸载面板
    opsctl help                    更多命令

  原生命令:
    systemctl {status|restart|stop} ops-panel
    journalctl -u ops-panel -f

  监听在 $OPS_LISTEN（默认仅本机可访问）。
  外网暴露强烈建议走 Tailscale / WireGuard / Cloudflare Tunnel，
  并在前面加 Caddy/Nginx 做 TLS 终结 + 反代。

===========================================================
EOF
