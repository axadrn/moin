#!/bin/sh
# Installs moin on one server: the control plane, and next to it the agent
# that runs your apps. Run it as root on a fresh Ubuntu or Debian server:
#
#   curl -fsSL https://raw.githubusercontent.com/axadrn/moin/main/controlplane/deploy/install.sh | sh -s -- --domain moin.example.com --email you@example.com
#
# Point the domain at this server first: an A record, so Let's Encrypt can
# give it a certificate. Run it again any time; it keeps what is there.
#
# MOIN_DEPLOY_DIR copies the deploy files from a folder instead of GitHub,
# MOIN_DEPLOY_REF fetches them from another branch or tag, and MOIN_IMAGE
# and MOIN_PULL=never run a control plane image built on this server.
set -eu

usage() {
	echo "usage: install.sh --domain moin.example.com --email you@example.com" >&2
	exit 1
}
domain=""
email=""
while [ $# -gt 0 ]; do
	case "$1" in
	--domain) domain=${2:-}; shift 2 || usage ;;
	--email) email=${2:-}; shift 2 || usage ;;
	*) usage ;;
	esac
done
[ -n "$domain" ] && [ -n "$email" ] || usage
[ "$(id -u)" = 0 ] || { echo "Run as root, or put sudo before sh." >&2; exit 1; }

deploy=/opt/moin
dir=/etc/moin-controlplane
env=$dir/env
mkdir -p "$deploy"
if [ -n "${MOIN_DEPLOY_DIR:-}" ]; then
	cp "$MOIN_DEPLOY_DIR"/*.sh "$MOIN_DEPLOY_DIR"/*.yml "$deploy/"
else
	command -v curl >/dev/null || { apt-get update -q && apt-get install -yq curl; }
	for file in setup.sh compose.yml compose.same-server.yml update.sh updater.sh; do
		curl -fsSL "https://raw.githubusercontent.com/axadrn/moin/${MOIN_DEPLOY_REF:-main}/controlplane/deploy/$file" -o "$deploy/$file"
	done
fi

# Docker and security updates.
sh "$deploy/setup.sh" >/dev/null

# The settings, once. Email and a bucket can follow; until then sign-in
# links go to the log, and the control plane keeps no backup of itself.
if [ ! -f "$env" ]; then
	(
		umask 077
		cat >"$env" <<EOF
APP_ENV=production
APP_URL=https://$domain
BILLING=false
SAME_SERVER=true
# The first address is the admin; only these may sign up.
ADMIN_EMAILS=$email
DB_CONNECTION=./data/app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
# Seals the secrets of your apps. Keep a copy somewhere safe.
SECRETS_KEY=$(openssl rand -base64 32)
# The proxy on this server is a Docker container.
TRUSTED_PROXY_CIDRS=172.16.0.0/12
# Email for sign-in links and invitations: Cloudflare Email Sending.
EMAIL_FROM=moin@$domain
CLOUDFLARE_ACCOUNT_ID=
CLOUDFLARE_API_TOKEN=
# A bucket for the control plane's own backup, then BACKUP_DATABASE=true.
S3_ENDPOINT=
S3_REGION=auto
S3_BUCKET=
S3_ACCESS_KEY=
S3_SECRET_KEY=
EOF
	)
fi
chmod 600 "$env"

cd "$deploy"
sh update.sh
echo "Waiting for the control plane…"
i=0
until curl -fsS http://127.0.0.1:8090/healthz >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || { echo "The control plane did not start: docker compose -f $deploy/compose.yml logs controlplane" >&2; exit 1; }
	sleep 2
done

# The admin's account, a workspace and this server; then the agent, which
# reaches the control plane on this machine and starts the proxy.
token=$(docker compose -f compose.yml exec -T controlplane ./main bootstrap "$(hostname)")
if [ -n "$token" ]; then
	curl -fsSL http://127.0.0.1:8090/install.sh | MOIN_URL=http://127.0.0.1:8090 sh -s -- "$token"
fi
echo "Waiting for the agent's proxy…"
i=0
until docker network inspect moin-proxy >/dev/null 2>&1 && [ "$(docker inspect -f '{{.State.Running}}' moin-traefik 2>/dev/null)" = true ]; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || { echo "The proxy did not start: journalctl -u moin-agent" >&2; exit 1; }
	sleep 2
done

# The proxy serves the control plane on its domain now.
sh update.sh

echo
echo "moin runs. Open https://$domain and sign in with $email."
if grep -q '^CLOUDFLARE_ACCOUNT_ID=$' "$env"; then
	echo "Email is not set up yet, so here is a sign-in link, good for a few minutes:"
	docker compose -f compose.yml exec -T controlplane ./main sign-in-link
	echo "Later: docker compose -f $deploy/compose.yml exec controlplane ./main sign-in-link"
fi
missing=$(grep -E '^(CLOUDFLARE_ACCOUNT_ID|CLOUDFLARE_API_TOKEN|S3_BUCKET|S3_ACCESS_KEY|S3_SECRET_KEY)=$' "$env" | cut -d= -f1 | tr '\n' ' ')
if [ -n "$missing" ]; then
	echo "Still empty in $env: $missing"
	echo "Fill them in for email and the control plane's own backup, then run: sh $deploy/update.sh"
fi
