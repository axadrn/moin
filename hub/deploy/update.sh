#!/bin/sh
# Starts the moin hub, or updates it to the newest image. Run it
# again for every update. MOIN_IMAGE picks another image, such as a version
# tag; service names after it update only those, as the updater does.
#
# Settings are in /etc/moin-hub/env. Keep them in 1Password
# instead: put a service account token in /etc/moin-hub/op-token
# and write every file as NAME.tpl with op:// references; this script fills
# them in as NAME before it starts.
set -eu
dir=/etc/moin-hub
data=/var/lib/moin-hub
cd "$(dirname "$0")"
MOIN_PULL=${MOIN_PULL:-always}
[ -n "${MOIN_IMAGE:-}" ] && export MOIN_IMAGE || unset MOIN_IMAGE

# Up to v1.0.0-beta.25 the hub was called the control plane. Once, its
# containers stop, and its settings and data move to the hub's folders.
# setup.sh may have made the new folders already, empty.
move() {
	rmdir "$2" 2>/dev/null || true
	if [ -e "$2" ]; then
		echo "$1 and $2 both have files: $2 counts, check $1." >&2
	else
		mv "$1" "$2"
	fi
}
if [ -d /etc/moin-controlplane ] && [ ! -f "$dir/env" ] && [ ! -f "$dir/env.tpl" ]; then
	if [ -f /.dockerenv ]; then
		echo "Run update.sh once on the server, as root: it moves the control plane to the hub." >&2
		exit 1
	fi
	echo "Moving the control plane to the hub…"
	# Every container of the compose project whose control plane uses that
	# data: it may have run from another folder, such as a git clone's
	# controlplane/deploy.
	for id in $(docker ps -aq --filter label=com.docker.compose.service=controlplane); do
		docker inspect -f '{{range .Mounts}}{{.Source}} {{end}}' "$id" | grep -q /var/lib/moin-controlplane || continue
		project=$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$id")
		old=$(docker ps -aq --filter "label=com.docker.compose.project=$project")
		# shellcheck disable=SC2086
		docker stop $old >/dev/null && docker rm $old >/dev/null
	done
	move /etc/moin-controlplane "$dir"
	[ ! -d /var/lib/moin-controlplane ] || move /var/lib/moin-controlplane "$data"
	# The settings name files under /run/moin-controlplane, such as the
	# GitHub App key.
	for file in "$dir"/env "$dir"/*.tpl; do
		[ ! -f "$file" ] || sed -i 's|/run/moin-controlplane/|/run/moin-hub/|g' "$file"
	done
fi

if [ -f "$dir/op-token" ]; then
	umask 077
	for template in "$dir"/*.tpl; do
		[ -e "$template" ] || continue
		docker run --rm -i -e OP_SERVICE_ACCOUNT_TOKEN="$(cat "$dir/op-token")" 1password/op:2 \
			op inject < "$template" > "${template%.tpl}.new"
		mv "${template%.tpl}.new" "${template%.tpl}"
	done
fi

if [ ! -f "$dir/env" ]; then
	echo "No settings: write $dir/env first, see the self-hosting guide." >&2
	exit 1
fi

profiles=""
grep -q '^TUNNEL_TOKEN=.' "$dir/env" && profiles=tunnel
[ -f updater.sh ] && profiles="${profiles:+$profiles,}updater"

# On the same server as the apps, the daemon's proxy serves the hub
# on its domain, once the daemon has started it. Its network is
# moin-proxy.
files="-f compose.yml"
if grep -q '^SAME_SERVER=true' "$dir/env"; then
	MOIN_DOMAIN=$(sed -n 's|^APP_URL=https*://\([^/:]*\).*|\1|p' "$dir/env" | head -n 1)
	export MOIN_DOMAIN
	if docker network inspect moin-proxy >/dev/null 2>&1; then
		export MOIN_PROXY_NETWORK=moin-proxy
		files="$files -f compose.same-server.yml"
	else
		echo "The daemon's proxy does not run yet: the hub answers on 127.0.0.1:8090 only." >&2
	fi
fi

mkdir -p "$data"
# shellcheck disable=SC2086
COMPOSE_PROFILES=$profiles docker compose $files up -d --pull "$MOIN_PULL" --remove-orphans "$@"
echo "moin hub started."
