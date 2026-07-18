#!/bin/sh
set -eu

BINARY=${1:-./kvmctl}
[ "$(id -u)" -eq 0 ] || { echo "install.sh must run as root" >&2; exit 1; }
[ -f "$BINARY" ] || { echo "kvmctl binary not found: $BINARY" >&2; exit 1; }

getent group kvmctl >/dev/null || groupadd --system kvmctl
id kvmctl >/dev/null 2>&1 || useradd --system --gid kvmctl --home-dir /nonexistent --shell /usr/sbin/nologin kvmctl

install -m 0755 "$BINARY" /usr/local/bin/kvmctl
install -d -m 0755 /usr/local/lib/kvmctl /etc/kvmctl
install -m 0755 scripts/setup-gadget.sh /usr/local/lib/kvmctl/setup-gadget.sh
if [ ! -e /etc/kvmctl/config.toml ]; then
    install -m 0640 -o root -g kvmctl packaging/config.example.toml /etc/kvmctl/config.toml
fi
install -m 0644 packaging/kvmctl.service /etc/systemd/system/kvmctl.service
install -m 0644 packaging/kvmctl-gadget.service /etc/systemd/system/kvmctl-gadget.service

systemctl daemon-reload
echo "Installed. Review /etc/kvmctl/config.toml, then run:"
echo "  sudo systemctl enable --now kvmctl.service"
