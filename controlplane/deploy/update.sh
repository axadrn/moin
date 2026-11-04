#!/bin/sh
# Starts the moin control plane, or updates it to the newest image. Run it
# again for every update. MOIN_IMAGE picks another image, such as a version
# tag; service names after it update only those, as the updater does.
#
# Settings are in /etc/moin-controlplane/env. Keep them in 1Password
# instead: put a service account token in /etc/moin-controlplane/op-token
# and write every file as NAME.tpl with op:// references; this script fills
# them in as NAME before it starts.
set -eu
dir=/etc/moin-controlplane
data=/var/lib/moin-controlplane
cd "$(dirname "$0")"
MOIN_PULL=${MOIN_PULL:-always}
[ -n "${MOIN_IMAGE:-}" ] && export MOIN_IMAGE || unset MOIN_IMAGE

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
# moin-proxy.
files="-f compose.yml"
if grep -q '^SAME_SERVER=true' "$dir/env"; then
	MOIN_DOMAIN=$(sed -n 's|^APP_URL=https*://\([^/:]*\).*|\1|p' "$dir/env" | head -n 1)
	export MOIN_DOMAIN
	if docker network inspect moin-proxy >/dev/null 2>&1; then
		export MOIN_PROXY_NETWORK=moin-proxy
		files="$files -f compose.same-server.yml"
	else
		echo "The agent's proxy does not run yet: the control plane answers on 127.0.0.1:8090 only." >&2
	fi
fi

mkdir -p "$data"
# shellcheck disable=SC2086
COMPOSE_PROFILES=$profiles docker compose $files up -d --pull "$MOIN_PULL" --remove-orphans "$@"
echo "moin control plane started."
