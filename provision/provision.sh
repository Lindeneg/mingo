#!/usr/bin/env bash
# Provisions a fresh Ubuntu 24.04 or 26.04 machine for mingo. Idempotent, runs as root.
#
#   sudo MINGO_USER=you MINGO_SSH_PUBKEY="ssh-ed25519 AAAA... you@pc" ./provision.sh
#
#   MINGO_USER              the user the sessions run as, which must be set
#   MINGO_SSH_PUBKEY        one public key for that user
#   MINGO_SSH_PUBKEY_FILE   a file of public keys, one per line
#   MINGO_NODE_MAJOR        the Node.js major from NodeSource, default 24
#   MINGO_GO_VERSION        the Go release from go.dev, default 1.27.3
#   MINGO_PLAYWRIGHT        the Playwright version whose system dependencies are installed, default 1.64.0, none to skip
#   MINGO_SWAP_GB           the swap file in GB, default 4, 0 for none
#   MINGO_SKIP_CLAUDE=1     do not install the Claude Code CLI
#
# Logging in to GitHub and Claude and setting the git identity are left to you.
set -euo pipefail

log() { printf '\n==> %s\n' "$*"; }
die() {
    printf 'provision: %s\n' "$*" >&2
    exit 1
}

[[ $(id -u) -eq 0 ]] || die "run as root"
. /etc/os-release
[[ ${ID:-} == ubuntu ]] || die "Ubuntu only, found ${ID:-unknown}"
case "${VERSION_ID:-}" in
    24.04 | 26.04) ;;
    *) printf 'provision: written for Ubuntu 24.04 and 26.04, found %s, continuing\n' "${VERSION_ID:-?}" ;;
esac

USER_NAME=${MINGO_USER:-}
[[ -n $USER_NAME ]] || die "set MINGO_USER to the user the sessions run as"
# the name goes into useradd, sudoers and sshd_config, so it is a plain user name or nothing
[[ $USER_NAME =~ ^[a-z_][a-z0-9_-]{0,31}$ ]] || die "MINGO_USER is $USER_NAME, use a plain user name of lowercase letters, digits, - and _"
NODE_MAJOR=${MINGO_NODE_MAJOR:-24}
GO_VERSION=${MINGO_GO_VERSION:-1.27.3}
PLAYWRIGHT_VERSION=${MINGO_PLAYWRIGHT:-1.64.0}
SWAP_GB=${MINGO_SWAP_GB:-4}
ARCH=$(dpkg --print-architecture)
CODENAME=${UBUNTU_CODENAME:-${VERSION_CODENAME:-noble}}
export DEBIAN_FRONTEND=noninteractive

# the image's own update jobs may hold the package lock at first boot
install -d /etc/apt/apt.conf.d
printf 'DPkg::Lock::Timeout "600";\n' >/etc/apt/apt.conf.d/90mingo-lock-timeout

log "Base packages"
apt-get update -q
apt-get install -y -q ca-certificates curl gnupg git tmux jq ripgrep unzip \
    build-essential python3 ufw fail2ban unattended-upgrades htop

log "User $USER_NAME"
id "$USER_NAME" >/dev/null 2>&1 || adduser --disabled-password --gecos "" "$USER_NAME"
HOME_DIR=$(getent passwd "$USER_NAME" | cut -d: -f6)
install -d -m 0700 -o "$USER_NAME" -g "$USER_NAME" "$HOME_DIR/.ssh"
AUTH=$HOME_DIR/.ssh/authorized_keys
touch "$AUTH"
chown "$USER_NAME:$USER_NAME" "$AUTH"
chmod 0600 "$AUTH"
add_key() {
    case "$1" in "" | \#*) return 0 ;; esac
    grep -qxF "$1" "$AUTH" || printf '%s\n' "$1" >>"$AUTH"
}
add_keys_from() {
    [[ -s $1 ]] || return 0
    while IFS= read -r line || [[ -n $line ]]; do add_key "$line"; done <"$1"
}
add_key "${MINGO_SSH_PUBKEY:-}"
[[ -n ${MINGO_SSH_PUBKEY_FILE:-} ]] && add_keys_from "$MINGO_SSH_PUBKEY_FILE"
# a cloud console puts the chosen key on root, and root login is turned off below
add_keys_from /root/.ssh/authorized_keys
[[ -s $AUTH ]] || die "no SSH public key for $USER_NAME, set MINGO_SSH_PUBKEY or MINGO_SSH_PUBKEY_FILE"
# passwordless sudo and docker make the user root equivalent, accepted on a machine that holds only the projects
printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$USER_NAME" >/etc/sudoers.d/90-mingo
chmod 0440 /etc/sudoers.d/90-mingo
visudo -cf /etc/sudoers.d/90-mingo >/dev/null

log "sshd, key only, one user, room for the desktop app's channels"
# the first value of a keyword wins and this file sorts before the cloud image's own
cat >/etc/ssh/sshd_config.d/10-mingo.conf <<EOF
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
AllowUsers $USER_NAME
MaxSessions 50
ClientAliveInterval 30
ClientAliveCountMax 6
EOF
if ! grep -qsE '^\s*Subsystem\s+sftp' /etc/ssh/sshd_config /etc/ssh/sshd_config.d/*.conf; then
    echo 'Subsystem sftp /usr/lib/openssh/sftp-server' >>/etc/ssh/sshd_config.d/10-mingo.conf
fi
sshd -t
systemctl restart ssh

log "logind, sessions outlive their SSH connection and the user's services outlive logout"
install -d /etc/systemd/logind.conf.d
printf '[Login]\nKillUserProcesses=no\n' >/etc/systemd/logind.conf.d/10-mingo.conf
loginctl enable-linger "$USER_NAME"

if [[ $SWAP_GB != 0 ]]; then
    log "Swap file, $SWAP_GB GB, used late"
    # built under another name and moved into place, so a run that dies halfway leaves nothing mistaken for swap
    if [[ ! -f /swapfile ]] || ! blkid -p -n swap /swapfile >/dev/null 2>&1; then
        (
            umask 077
            rm -f /swapfile.new
            fallocate -l "${SWAP_GB}G" /swapfile.new
            mkswap /swapfile.new >/dev/null
        )
        mv -f /swapfile.new /swapfile
    fi
    swapon --show=NAME --noheadings | grep -qx /swapfile || swapon /swapfile
    grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >>/etc/fstab
    printf 'vm.swappiness = 10\n' >/etc/sysctl.d/90-mingo-swap.conf
    sysctl -q -p /etc/sysctl.d/90-mingo-swap.conf
fi

log "Firewall, brute force protection, unattended security upgrades"
ufw default deny incoming >/dev/null
ufw default allow outgoing >/dev/null
ufw allow OpenSSH >/dev/null
ufw --force enable >/dev/null
printf '[sshd]\nenabled = true\nmaxretry = 5\nbantime = 1h\n' >/etc/fail2ban/jail.d/10-mingo.conf
systemctl enable --now fail2ban >/dev/null
cat >/etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF

log "Apt repositories for Docker, NodeSource and the GitHub CLI"
install -d -m 0755 /etc/apt/keyrings /usr/share/keyrings
if [[ ! -f /etc/apt/keyrings/docker.asc ]]; then
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
fi
cat >/etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: $CODENAME
Components: stable
Architectures: $ARCH
Signed-By: /etc/apt/keyrings/docker.asc
EOF
if [[ ! -f /usr/share/keyrings/nodesource.gpg ]]; then
    curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /usr/share/keyrings/nodesource.gpg
    chmod 0644 /usr/share/keyrings/nodesource.gpg
fi
cat >/etc/apt/sources.list.d/nodesource.sources <<EOF
Types: deb
URIs: https://deb.nodesource.com/node_$NODE_MAJOR.x
Suites: nodistro
Components: main
Architectures: $ARCH
Signed-By: /usr/share/keyrings/nodesource.gpg
EOF
if [[ ! -f /etc/apt/keyrings/githubcli-archive-keyring.gpg ]]; then
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg
    chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg
fi
printf 'deb [arch=%s signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main\n' "$ARCH" \
    >/etc/apt/sources.list.d/github-cli.list
apt-get update -q

log "Docker Engine, Node.js $NODE_MAJOR, GitHub CLI"
# docker publishes ports past ufw, so they bind to loopback unless a compose file names another address
if [[ ! -f /etc/docker/daemon.json ]]; then
    install -d /etc/docker
    printf '{\n  "ip": "127.0.0.1"\n}\n' >/etc/docker/daemon.json
fi
apt-get install -y -q docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin nodejs gh
systemctl enable --now docker >/dev/null
usermod -aG docker "$USER_NAME"
# node 25 and later no longer ship corepack, which pnpm projects install pnpm through
command -v corepack >/dev/null || npm install -g --silent corepack
corepack enable

log "Go $GO_VERSION"
if [[ $(/usr/local/go/bin/go env GOVERSION 2>/dev/null) != "go$GO_VERSION" ]]; then
    go_tarball=go$GO_VERSION.linux-$ARCH.tar.gz
    go_sum=$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' |
        jq -r --arg f "$go_tarball" '[.[].files[] | select(.filename == $f) | .sha256][0] // empty')
    [[ -n $go_sum ]] || die "go.dev/dl lists no $go_tarball"
    curl -fsSLo "/tmp/$go_tarball" "https://go.dev/dl/$go_tarball"
    echo "$go_sum  /tmp/$go_tarball" | sha256sum -c --quiet - || die "$go_tarball does not match its SHA-256 on go.dev/dl"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "/tmp/$go_tarball"
    rm -f "/tmp/$go_tarball"
fi
# shellcheck disable=SC2016
printf 'export PATH="/usr/local/go/bin:$PATH"\n' >/etc/profile.d/go.sh

if [[ $PLAYWRIGHT_VERSION != none ]]; then
    log "Playwright $PLAYWRIGHT_VERSION system dependencies"
    npx --yes "playwright@$PLAYWRIGHT_VERSION" install-deps
fi

if [[ ${MINGO_SKIP_CLAUDE:-0} != 1 ]]; then
    log "Claude Code CLI for $USER_NAME"
    su - "$USER_NAME" -c 'test -x ~/.local/bin/claude' || su - "$USER_NAME" -c 'curl -fsSL https://claude.ai/install.sh | bash'
fi

log "Folders"
su - "$USER_NAME" -c 'mkdir -p ~/dev ~/.claude'

log "Done"
cat <<EOF
Provisioned for $USER_NAME. Next, as $USER_NAME on this machine:
  gh auth login, git config --global user.name and user.email, claude auth login,
  then clone mingo into ~/dev/mingo, run go build -o ~/.local/bin/mingo ./cmd/mingo there
  and mingo install (see its README).
On a machine provisioned by hand, reboot once so logind reads its new setting.
EOF
