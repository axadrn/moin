#!/usr/bin/env bash
# Rents throwaway servers at Hetzner, runs a test against them and deletes them
# again, even when the test fails or is cancelled.
#
#   scripts/test-servers.sh <count> [command...]
#
# The command runs on this machine. It finds the servers' addresses in SERVERS
# and the ssh options in SSH_OPTIONS:
#
#   scripts/test-servers.sh 2 'for s in $SERVERS; do ssh $SSH_OPTIONS root@$s docker version; done'
#
# Without a command, it logs in to each server and prints its architecture.
# Needs hcloud with a context named moin-test: hcloud context create moin-test
set -euo pipefail

count=${1:-}
if [[ ! $count =~ ^[1-9][0-9]*$ ]]; then
    echo 'Usage: scripts/test-servers.sh <count> [command...]' >&2
    exit 2
fi
shift

export HCLOUD_CONTEXT=moin-test
key=$HOME/.ssh/id_ed25519.pub
run=$(date +%s)-$$

delete_servers() {
    local servers
    servers=$(hcloud server list -l "test-run=$run" -o noheader -o columns=id)
    if [ -n "$servers" ]; then
        # shellcheck disable=SC2086 # one argument per server
        hcloud server delete $servers
    fi
    echo "Servers left from this run: $(hcloud server list -l "test-run=$run" -o noheader -o columns=id | wc -w)"
}
trap delete_servers EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# Hetzner needs the public key once, under any name.
fingerprint=$(ssh-keygen -E md5 -lf "$key" | awk '{print $2}' | cut -d: -f2-)
key_name=$(hcloud ssh-key list -o noheader -o columns=fingerprint,name | awk -v f="$fingerprint" '$1 == f {print $2}')
if [ -z "$key_name" ]; then
    key_name=moin-test
    hcloud ssh-key create --name "$key_name" --public-key-from-file "$key" >/dev/null
fi

create_server() {
    local name=$1 type location
    for type in "${types[@]}"; do
        for location in "${locations[@]}"; do
            if hcloud server create --name "$name" --type "$type" --location "$location" \
                --image ubuntu-24.04 --ssh-key "$key_name" --label "test-run=$run" >/dev/null; then
                # All servers of a run get the same architecture.
                types=("$type")
                locations=("$location")
                echo "$name: $type in $location"
                return
            fi
        done
    done
    echo "$name: no server could be booked." >&2
    exit 1
}

types=(cax11 cx23)
locations=(fsn1 nbg1 hel1)
SERVERS=
for i in $(seq "$count"); do
    create_server "test-$run-$i"
    SERVERS+="$(hcloud server ip "test-$run-$i") "
done
export SERVERS=${SERVERS% }

# Hetzner reuses addresses, so these hosts must never reach known_hosts.
export SSH_OPTIONS='-o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR'

for server in $SERVERS; do
    tries=0
    # shellcheck disable=SC2086 # one word per option
    until ssh $SSH_OPTIONS "root@$server" true; do
        tries=$((tries + 1))
        if [ "$tries" -ge 30 ]; then
            echo "Could not reach a server over ssh." >&2
            exit 1
        fi
        sleep 5
    done
    # The first boot holds the package manager's lock until it is done.
    # shellcheck disable=SC2086 # one word per option
    ssh $SSH_OPTIONS "root@$server" cloud-init status --wait >/dev/null || true
done

if [ "$#" -gt 0 ]; then
    bash -c "$*"
else
    for server in $SERVERS; do
        # shellcheck disable=SC2086 # one word per option
        ssh $SSH_OPTIONS "root@$server" uname -m
    done
fi
