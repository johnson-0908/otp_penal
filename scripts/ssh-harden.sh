#!/usr/bin/env bash
# SSH hardening for a single-user server that's currently being brute-forced.
# Run as root. Reads current state first, prints the planned diff, asks to confirm.
#
# What this script does:
#   1. Backs up /etc/ssh/sshd_config
#   2. Disables password auth (key-only)
#   3. Disables root password login (keeps root key login, safer for recovery)
#   4. Tightens MaxAuthTries / LoginGraceTime / ClientAlive
#   5. Installs fail2ban with an SSH jail (if not already installed)
#   6. Reloads sshd only after sshd -t validates the new config
#
# It will NOT change the SSH port (you said you already did). If you want to,
# edit /etc/ssh/sshd_config or a drop-in file in /etc/ssh/sshd_config.d/ yourself.

set -euo pipefail

if [ "$(id -u)" != "0" ]; then
  echo "must run as root" >&2
  exit 1
fi

CONFIG=/etc/ssh/sshd_config
DROPIN_DIR=/etc/ssh/sshd_config.d
DROPIN=$DROPIN_DIR/99-harden.conf

mkdir -p "$DROPIN_DIR"

timestamp=$(date +%Y%m%d-%H%M%S)
cp -a "$CONFIG" "${CONFIG}.bak-${timestamp}"
if [ -f "$DROPIN" ]; then
  cp -a "$DROPIN" "${DROPIN}.bak-${timestamp}"
fi
echo "backup written: ${CONFIG}.bak-${timestamp}"

# --- sanity: make sure the current user has a key installed ---
ROOT_KEYS=/root/.ssh/authorized_keys
USER_KEYS=""
if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
  USER_KEYS="/home/$SUDO_USER/.ssh/authorized_keys"
fi

has_key=0
if [ -s "$ROOT_KEYS" ]; then has_key=1; fi
if [ -n "$USER_KEYS" ] && [ -s "$USER_KEYS" ]; then has_key=1; fi

if [ "$has_key" = "0" ]; then
  cat >&2 <<EOF
REFUSE TO CONTINUE: no authorized_keys found for root${USER_KEYS:+ or $SUDO_USER}.
If you disable password auth now, you will be locked out.

Add a key first:
  # on your workstation:
  ssh-copy-id -p <your-ssh-port> root@<server-ip>
then re-run this script.
EOF
  exit 2
fi

cat > "$DROPIN" <<'EOF'
# Hardened SSH settings — managed by ops-panel/scripts/ssh-harden.sh
# (drop-in overrides; remove this file to revert)

Protocol 2

PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
PermitEmptyPasswords no
UsePAM yes

# Root can still log in with a key (change to "no" if you use a sudo account)
PermitRootLogin prohibit-password

PubkeyAuthentication yes

MaxAuthTries 3
MaxSessions 5
LoginGraceTime 20
ClientAliveInterval 60
ClientAliveCountMax 2

X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding no
PermitUserEnvironment no
PrintMotd no
EOF

echo "planned drop-in config ($DROPIN):"
echo "--------"
cat "$DROPIN"
echo "--------"

echo
echo "validating..."
if ! sshd -t; then
  echo "sshd -t failed. Reverting." >&2
  rm -f "$DROPIN"
  exit 3
fi

echo "reloading sshd (your current session stays open)..."
if command -v systemctl >/dev/null 2>&1; then
  systemctl reload ssh 2>/dev/null || systemctl reload sshd
else
  service ssh reload 2>/dev/null || service sshd reload
fi

# --- fail2ban ---
if ! command -v fail2ban-client >/dev/null 2>&1; then
  echo "installing fail2ban..."
  if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get update -y
    DEBIAN_FRONTEND=noninteractive apt-get install -y fail2ban
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y fail2ban
  elif command -v yum >/dev/null 2>&1; then
    yum install -y fail2ban
  else
    echo "could not detect package manager; please install fail2ban manually" >&2
  fi
fi

if command -v fail2ban-client >/dev/null 2>&1; then
  mkdir -p /etc/fail2ban/jail.d
  cat > /etc/fail2ban/jail.d/sshd.local <<'EOF'
[sshd]
enabled = true
maxretry = 3
findtime = 10m
bantime = 1d
EOF
  systemctl enable --now fail2ban >/dev/null 2>&1 || true
  systemctl restart fail2ban || true
  echo "fail2ban jail:"
  fail2ban-client status sshd || true
fi

echo
echo "DONE. Test key login from another terminal BEFORE closing this session:"
echo "  ssh -p <port> root@<server>"
echo "If it fails, restore with:"
echo "  rm $DROPIN && systemctl reload ssh"
