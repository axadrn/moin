#!/bin/sh
# Prepares an Ubuntu or Debian server for the moin hub: Docker, security
# updates and, with --tailscale, Tailscale with SSH on your tailnet.
# install.sh runs it. Run it again any time, as root.
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

# The deploy files and settings, the files they point to, and the
# database. The last two are for root only.
mkdir -p /opt/moin /etc/moin-hub /var/lib/moin-hub
chmod 700 /etc/moin-hub /var/lib/moin-hub
echo "Ready."
