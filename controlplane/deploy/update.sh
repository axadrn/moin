#!/bin/sh
# Starts the ops control plane, or updates it to the newest image. Run it
# again for every update. OPS_IMAGE picks another image, such as a version
# tag; service names after it update only those, as the updater does.
#
# Settings are in /etc/ops-controlplane/env. Keep them in 1Password instead:
# put a service account token in /etc/ops-controlplane/op-token and write
# every file as NAME.tpl with op:// references; this script fills them in
# as NAME before it starts.
set -eu
dir=/etc/ops-controlplane
cd "$(dirname "$0")"

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
mkdir -p /var/lib/ops-controlplane
COMPOSE_PROFILES=$profiles docker compose up -d --pull "${OPS_PULL:-always}" --remove-orphans "$@"
echo "ops control plane started."
