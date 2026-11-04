#!/bin/sh
# The updater: "Update now" in moin. It runs next to the control plane in
# compose.yml, the one part with access to Docker; the control plane has
# none. It waits for the file update/request in the data directory, which
# the control plane writes, then runs update.sh with the requested version.
# If that version does not answer within two minutes, it starts the old one
# again. It reports in update/status and touches update/updater every few
# seconds, so the control plane knows it runs.
set -u
data=${MOIN_UPDATE_DIR:-/var/lib/moin-controlplane/update}
deploy=${MOIN_DEPLOY_DIR:-/deploy}
mkdir -p "$data"

# The compose project of the control plane, from this container's label, so
# update.sh changes that project from here.
COMPOSE_PROJECT_NAME=$(docker inspect -f '{{ index .Config.Labels "com.docker.compose.project" }}' "$(hostname)")
export COMPOSE_PROJECT_NAME

# status writes the state of an update: state, version, from, reason.
status() {
	{
		echo "state=$1"
		echo "version=$2"
		echo "from=$3"
		echo "reason=$4"
		echo "at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	} >"$data/status.new"
	mv "$data/status.new" "$data/status"
}

# value reads key from a file of key=value lines.
value() {
	sed -n "s/^$1=//p" "$2" | head -n 1
}

# answers waits up to two minutes for the control plane to answer with
# version.
answers() {
	tries=0
	while [ "$tries" -lt 24 ]; do
		if wget -q -T 5 -O - http://controlplane:8090/healthz >/dev/null 2>&1 &&
			wget -q -T 5 -O - http://controlplane:8090/api/v1/meta 2>/dev/null | grep -q "\"version\":\"$1\""; then
			return 0
		fi
		sleep 5
		tries=$((tries + 1))
	done
	return 1
}

update() {
	mv "$data/request" "$data/running" || return
	version=$(value version "$data/running")
	from=$(value from "$data/running")
	case "$version" in
	*[!A-Za-z0-9.+-]*) bad=1 ;;
	v[0-9]*) bad=0 ;;
	*) bad=1 ;;
	esac
	if [ "$bad" = 1 ]; then
		status failed "$version" "$from" "this is no release"
		mv "$data/running" "$data/finished"
		return
	fi
	status updating "$version" "$from" ""
	# Keep the running image, to start it again if the new one fails.
	container=$(docker compose -f "$deploy/compose.yml" ps -q controlplane)
	image=$(docker inspect -f '{{.Config.Image}}' "$container")
	# The repository without its tag; a registry port has a colon too.
	repo=$image
	case "${image##*/}" in *:*) repo=${image%:*} ;; esac
	docker tag "$(docker inspect -f '{{.Image}}' "$container")" "$repo:rollback"
	echo "$(date -u) update from $from to $version" >"$data/log"
	if ! MOIN_IMAGE="$repo:$version" sh "$deploy/update.sh" controlplane >>"$data/log" 2>&1; then
		reason="$version did not start: $(tail -n 1 "$data/log")"
	elif ! answers "$version"; then
		reason="$version did not answer within two minutes"
	else
		status done "$version" "$from" ""
		mv "$data/running" "$data/finished"
		return
	fi
	if MOIN_IMAGE="$repo:rollback" MOIN_PULL=never sh "$deploy/update.sh" controlplane >>"$data/log" 2>&1; then
		status rolled_back "$version" "$from" "$reason"
	else
		status failed "$version" "$from" "$reason, and $from did not start again: run sh update.sh on the server"
	fi
	mv "$data/running" "$data/finished"
}

# An update cut short, by a restart of the server, ended unknown.
if [ -f "$data/running" ]; then
	status failed "$(value version "$data/running")" "$(value from "$data/running")" "the updater stopped during the update: check the control plane on the server"
	mv "$data/running" "$data/finished"
fi

while :; do
	touch "$data/updater"
	[ -f "$data/request" ] && update
	sleep 5
done
