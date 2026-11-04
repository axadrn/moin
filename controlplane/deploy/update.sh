#!/bin/sh
# Starts the moin control plane, or updates it to the newest image. Run it
# again for every update. MOIN_IMAGE picks another image, such as a version
# tag; service names after it update only those, as the updater does.
#
# Settings are in /etc/moin-controlplane/env. Keep them in 1Password
# instead: put a service account token in /etc/moin-controlplane/op-token
# and write every file as NAME.tpl with op:// references; this script fills
# them in as NAME before it starts.
#
# A control plane installed under the old name (ops, until 2026-12-04)
# moves once: this script stops it, copies its settings and its data to the
# new folders and starts it from there. The old folders stay as they were.
set -eu
dir=/etc/moin-controlplane
data=/var/lib/moin-controlplane
cd "$(dirname "$0")"
# The old name's variables still work.
MOIN_IMAGE=${MOIN_IMAGE:-${OPS_IMAGE:-}}
MOIN_PULL=${MOIN_PULL:-${OPS_PULL:-always}}
[ -n "$MOIN_IMAGE" ] && export MOIN_IMAGE || unset MOIN_IMAGE

# The move from the old name, once: the new settings do not exist yet.
if [ ! -f "$dir/env" ] && [ ! -f "$dir/env.tpl" ] && [ -d /etc/ops-controlplane ]; then
	echo "Moving the control plane from its old name to moin."
	# The image first, so the control plane is away only for the copy.
	[ "$MOIN_PULL" = never ] || docker pull "${MOIN_IMAGE:-ghcr.io/axadrn/moin:stable}"
	# Stop the old one first, so its database is copied at rest.
	for project in $(docker ps -a --filter label=com.docker.compose.service=controlplane --format '{{.Label "com.docker.compose.project"}}' | sort -u); do
		echo "Stopping the old control plane ($project)."
		docker compose -p "$project" down --remove-orphans
	done
	mkdir -p -m 700 "$dir"
	cp -a /etc/ops-controlplane/. "$dir/"
	if [ -d /var/lib/ops-controlplane ]; then
		mkdir -p "$data"
		cp -a /var/lib/ops-controlplane/. "$data/"
	fi
	echo "Copied /etc/ops-controlplane to $dir and /var/lib/ops-controlplane to $data. The old folders stay; remove them once moin runs."
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

# On the same server as the apps, the agent's proxy serves the control
# plane on its domain, once the agent has started it. Its network is
# moin-proxy, or ops-proxy while the agent has not moved to its new name.
files="-f compose.yml"
if grep -q '^SAME_SERVER=true' "$dir/env"; then
	MOIN_DOMAIN=$(sed -n 's|^APP_URL=https*://\([^/:]*\).*|\1|p' "$dir/env" | head -n 1)
	export MOIN_DOMAIN
	MOIN_PROXY_NETWORK=""
	for network in moin-proxy ops-proxy; do
		if docker network inspect "$network" >/dev/null 2>&1; then
			MOIN_PROXY_NETWORK=$network
			break
		fi
	done
	if [ -n "$MOIN_PROXY_NETWORK" ]; then
		export MOIN_PROXY_NETWORK
		files="$files -f compose.same-server.yml"
	else
		echo "The agent's proxy does not run yet: the control plane answers on 127.0.0.1:8090 only." >&2
	fi
fi

mkdir -p "$data"
# shellcheck disable=SC2086
COMPOSE_PROFILES=$profiles docker compose $files up -d --pull "$MOIN_PULL" --remove-orphans "$@"
# The agent's network of the old name goes once the control plane left it
# and nothing else uses it; the proxy only stayed on it for the control
# plane.
if [ "${MOIN_PROXY_NETWORK:-}" = moin-proxy ] && docker network inspect ops-proxy >/dev/null 2>&1; then
	docker network disconnect ops-proxy moin-traefik >/dev/null 2>&1 || true
	docker network rm ops-proxy >/dev/null 2>&1 || docker network connect ops-proxy moin-traefik >/dev/null 2>&1 || true
fi
echo "moin control plane started."
