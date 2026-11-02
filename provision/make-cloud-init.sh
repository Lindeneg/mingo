#!/usr/bin/env bash
# Prints cloud-init user data for a new Ubuntu cloud server that installs the given public keys
# and runs provision.sh on the first boot, then reboots.
#
#   provision/make-cloud-init.sh ~/.ssh/server.pub [more.pub ...] > cloud-init.yaml
#
# MINGO_USER names the user the sessions run as, and must be set.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
(($# >= 1)) || {
    echo "usage: $0 <public-key-file> [more ...]" >&2
    exit 2
}
# a private key pasted into a cloud console would be stored there, so every line must be a public key
public_key='^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) [A-Za-z0-9+/]+=*( |$)'
for file in "$@"; do
    [[ -s $file ]] || {
        echo "not a key file: $file" >&2
        exit 2
    }
    if tr -d '\r' <"$file" | grep -v '^[[:space:]]*$' | grep -qvE "$public_key"; then
        echo "not a public key file, every line must be an OpenSSH public key: $file" >&2
        exit 2
    fi
done
user=${MINGO_USER:-}
[[ $user =~ ^[a-z_][a-z0-9_-]{0,31}$ ]] || {
    echo "set MINGO_USER to a plain user name, the user the sessions run as" >&2
    exit 2
}

indent() { sed 's/^/      /'; }

cat <<EOF
#cloud-config
# made by mingo's provision/make-cloud-init.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ), regenerate rather than edit
package_update: true
write_files:
  - path: /root/mingo/authorized_keys
    permissions: "0600"
    content: |
EOF
cat "$@" | tr -d '\r' | grep -v '^[[:space:]]*$' | indent
cat <<EOF
  - path: /root/mingo/provision.sh
    permissions: "0755"
    content: |
EOF
tr -d '\r' <"$here/provision.sh" | indent
cat <<EOF
runcmd:
  - [bash, -c, "MINGO_USER=$user MINGO_SSH_PUBKEY_FILE=/root/mingo/authorized_keys /root/mingo/provision.sh > /var/log/mingo-provision.log 2>&1"]
power_state:
  mode: reboot
  message: provisioned for mingo
  condition: true
EOF
