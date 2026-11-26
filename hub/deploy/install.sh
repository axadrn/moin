#!/bin/sh
# Installs a moin hub. Run it as root on a fresh Ubuntu or Debian server:
#
#   curl -fsSL https://raw.githubusercontent.com/axadrn/moin/main/hub/deploy/install.sh | sh -s -- --domain moin.example.com --email you@example.com
#
# Three ways:
#   (nothing)                          one server: the hub, the daemon and your apps
#   --hub-only                         only the hub, with its own Traefik for HTTPS
#   --hub-only --tunnel-token <token>  only the hub, behind a Cloudflare Tunnel, no open port
#
# Point the domain at this server first, with an A record, so Let's Encrypt
# gives it a certificate. A tunnel needs none: its public hostname points at
# http://hub:8090. Run it again any time, with the same flags: it keeps the
# settings and the data.
#
# MOIN_DEPLOY_DIR copies the deploy files from a folder instead of GitHub,
# MOIN_DEPLOY_REF fetches them from another branch or tag, and MOIN_IMAGE
# runs another hub image.
set -eu

usage() {
	echo "usage: install.sh --domain moin.example.com --email you@example.com [--hub-only [--tunnel-token <token>]]" >&2
	exit 1
}
domain=""
email=""
hub_only=false
tunnel_token=""
while [ $# -gt 0 ]; do
	case "$1" in
	--domain) domain=${2:-}; shift 2 || usage ;;
	--email) email=${2:-}; shift 2 || usage ;;
	--hub-only) hub_only=true; shift ;;
	--tunnel-token) tunnel_token=${2:-}; shift 2 || usage ;;
	*) usage ;;
	esac
done
[ -n "$domain" ] && [ -n "$email" ] || usage
[ -z "$tunnel_token" ] || [ "$hub_only" = true ] || { echo "--tunnel-token goes with --hub-only." >&2; exit 1; }
[ "$(id -u)" = 0 ] || { echo "Run as root, or put sudo before sh." >&2; exit 1; }

deploy=/opt/moin
env=$deploy/.env
files="setup.sh update.sh compose.yml compose.same-server.yml compose.traefik.yml compose.tunnel.yml"
# New settings bring a new SECRETS_KEY, which cannot open the secrets in a
# database that is here already.
if [ ! -f "$env" ] && [ -f /var/lib/moin-hub/app.db ]; then
	echo "A hub database is here, but no $env: put its settings there first." >&2
	exit 1
fi
mkdir -p "$deploy"
for file in $files; do
	if [ -n "${MOIN_DEPLOY_DIR:-}" ]; then
		cp "$MOIN_DEPLOY_DIR/$file" "$deploy/"
	else
		command -v curl >/dev/null || { apt-get update -q && apt-get install -yq curl; }
		curl -fsSL "https://raw.githubusercontent.com/axadrn/moin/${MOIN_DEPLOY_REF:-main}/hub/deploy/$file" -o "$deploy/$file"
	fi
done

# Docker, security updates and the folders.
sh "$deploy/setup.sh" >/dev/null

# The settings, once. COMPOSE_FILE picks the files of this setup. Email and
# a bucket can follow. Until then mail goes to the log, the admin signs in
# with a recovery key, and the hub keeps no backup of itself.
if [ "$hub_only" = false ]; then
	compose_file=compose.yml:compose.same-server.yml
elif [ -z "$tunnel_token" ]; then
	compose_file=compose.yml:compose.traefik.yml
else
	compose_file=compose.yml:compose.tunnel.yml
fi
new=false
if [ ! -f "$env" ]; then
	new=true
	(
		umask 077
		{
			echo "# The settings of the moin hub. Compose reads them, and so does the hub."
			echo "# A value with \$, # or a space goes in single quotes: KEY='a\$b'."
			echo "# After a change: sh $deploy/update.sh"
			echo "COMPOSE_FILE=$compose_file"
			echo "DOMAIN=$domain"
			[ "$hub_only" = true ] || echo "SAME_SERVER=true"
			[ -z "$tunnel_token" ] || echo "TUNNEL_TOKEN=$tunnel_token"
			cat <<SETTINGS
APP_ENV=production
BILLING=false
# The first address is the admin. Only these may sign up.
ADMIN_EMAILS=$email
DB_CONNECTION=./data/app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
# Seals the Cloudflare token, the alert targets and the GitHub App's keys. Keep a copy.
SECRETS_KEY=$(openssl rand -base64 32)
# Traefik or the tunnel reaches the hub through Docker's network.
TRUSTED_PROXY_CIDRS=172.16.0.0/12
# Email for invitations and alerts: Cloudflare Email Sending.
EMAIL_FROM=moin@$domain
CLOUDFLARE_ACCOUNT_ID=
CLOUDFLARE_API_TOKEN=
# A bucket for the hub's own backup.
S3_ENDPOINT=
S3_REGION=auto
S3_BUCKET=
S3_ACCESS_KEY=
S3_SECRET_KEY=
SETTINGS
		} >"$env"
	)
fi

cd "$deploy"
if [ "$hub_only" = false ] && ! docker network inspect moin-proxy >/dev/null 2>&1; then
	# The daemon's Traefik serves the hub, and it comes with the daemon
	# below. Until then the hub runs alone.
	COMPOSE_FILE=compose.yml sh update.sh
else
	sh update.sh
fi
echo "Waiting for the hub…"
i=0
until curl -fsS http://127.0.0.1:8090/healthz >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || { echo "The hub did not start: cd $deploy && docker compose logs hub" >&2; exit 1; }
	sleep 2
done

if [ "$hub_only" = false ]; then
	# The admin's account, a workspace and this server. Then the daemon,
	# which reaches the hub on this machine and starts its Traefik.
	token=$(docker compose exec -T hub ./main bootstrap "$(hostname)")
	if [ -n "$token" ]; then
		curl -fsSL http://127.0.0.1:8090/install.sh | MOIN_URL=http://127.0.0.1:8090 sh -s -- "$token"
	fi
	echo "Waiting for the daemon's Traefik…"
	i=0
	until docker network inspect moin-proxy >/dev/null 2>&1 && [ "$(docker inspect -f '{{.State.Running}}' moin-traefik 2>/dev/null)" = true ]; do
		i=$((i + 1))
		[ "$i" -lt 60 ] || { echo "Traefik did not start: journalctl -u moin-daemon" >&2; exit 1; }
		sleep 2
	done
	# Now Traefik serves the hub on its domain.
	sh update.sh
elif [ "$new" = true ]; then
	# The admin's account, a workspace and the first app server, web-1.
	token=$(docker compose exec -T hub ./main bootstrap web-1)
fi

echo
echo "moin runs at https://$domain. Sign in as $email with this key:"
docker compose exec -T hub ./main recovery-key "$email"
echo "A new key: cd $deploy && docker compose exec hub ./main recovery-key $email"
if [ "$hub_only" = true ] && [ -n "${token:-}" ]; then
	echo
	echo "Add your first app server, web-1. On it, as root:"
	echo "curl -fsSL https://$domain/install.sh | sh -s -- $token"
fi
missing=$(grep -E '^(CLOUDFLARE_ACCOUNT_ID|CLOUDFLARE_API_TOKEN|S3_BUCKET|S3_ACCESS_KEY|S3_SECRET_KEY)=$' "$env" | cut -d= -f1 | tr '\n' ' ')
if [ -n "$missing" ]; then
	echo
	echo "Still empty in $env: $missing"
	echo "Fill them in for email and the hub's own backup, then run: sh $deploy/update.sh"
fi
if [ "$hub_only" = false ]; then
	echo
	echo "The hub shares this server with your apps. For full separation, run it on its own server with --hub-only: https://moin.run/docs/self-hosting#which-way"
fi
