#!/bin/sh
# Prepares a fresh Ubuntu or Debian server for the moin control plane: Docker,
# security updates and, with --tailscale, Tailscale with SSH on your tailnet.
# Run it once as root.
set -eu

command -v docker >/dev/null || curl -fsSL https://get.docker.com | sh

apt-get install -y unattended-upgrades >/dev/null
dpkg-reconfigure -f noninteractive unattended-upgrades

if [ "${1:-}" = "--tailscale" ]; then
	command -v tailscale >/dev/null || curl -fsSL https://tailscale.com/install.sh | sh
	# The first run prints a login link.
	tailscale status >/dev/null 2>&1 || tailscale up --ssh
	tailscale set --auto-update
fi

mkdir -p -m 700 /etc/moin-controlplane
mkdir -p /var/lib/moin-controlplane
echo "Ready. Write /etc/moin-controlplane/env, then run update.sh."
