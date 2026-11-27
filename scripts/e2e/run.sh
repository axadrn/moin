#!/usr/bin/env bash
# The whole product on two real servers, in one command:
#
#   scripts/e2e/run.sh
#
# It builds moin, rents two servers with scripts/test-servers.sh and
# deletes them at the end, also when a step fails:
#
#   prod     the server you deploy to
#   second   the bucket (SeaweedFS), the watcher and a webhook for alerts
#
# Each step prints ✓ or ✗ with the reason. The first ✗ ends the run.
# Your ~/.ssh/config and ~/.config/moin stay as they are: ssh reads a
# config of this run, and moin a config folder of this run.
set -uo pipefail

repo=$(cd "$(dirname "$0")/../.." && pwd)
here=$repo/scripts/e2e

# ---------------------------------------------------------------- set up

# Without servers yet: build moin, rent two servers and run again on them.
if [ -z "${SERVERS:-}" ]; then
    E2E_WORK=$(mktemp -d "${TMPDIR:-/tmp}/moin-e2e.XXXXXX")
    export E2E_WORK
    echo "Building moin into $E2E_WORK"
    (cd "$repo" && go build -o "$E2E_WORK/moin" ./cmd/moin) || exit 1
    exec "$repo/scripts/test-servers.sh" 2 "$here/run.sh"
fi

work=$E2E_WORK
read -r prod_ip second_ip <<<"$SERVERS"
dashed() { echo "${1//./-}"; }
shop_url=https://shop.$(dashed "$prod_ip").sslip.io
hello_url=https://hello.$(dashed "$prod_ip").sslip.io
tools_url=https://it-tools.$(dashed "$prod_ip").sslip.io
watcher_url=https://watcher.$(dashed "$second_ip").sslip.io
watcher_password=e2e-watcher-password
bucket_secret=e2e-bucket-secret

# ssh prod, ssh prod-ada and ssh second, for moin and for this script.
cat >"$work/ssh_config" <<EOF
Host prod
  HostName $prod_ip
  User root
Host prod-ada
  HostName $prod_ip
  User ada
Host second
  HostName $second_ip
  User root
Host *
  BatchMode yes
  ConnectTimeout 10
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
  LogLevel ERROR
EOF
mkdir -p "$work/bin"
printf '#!/bin/sh\nexec %s -F %s "$@"\n' "$(command -v ssh)" "$work/ssh_config" >"$work/bin/ssh"
chmod +x "$work/bin/ssh"
export PATH=$work/bin:$PATH
export XDG_CONFIG_HOME=$work/config
# No command may wait for a terminal, unless the step gives it one.
exec </dev/null

started=$(date +%s)

# ---------------------------------------------------------------- helpers

pass() { echo "  ✓ $1"; }

# fail says what went wrong and ends the run; test-servers.sh then deletes
# the servers.
fail() {
    echo "  ✗ $1"
    shift
    [ $# -gt 0 ] && printf '%s\n' "$@" | tail -25 | sed 's/^/      /'
    echo "Failed after $(elapsed). Servers: prod $prod_ip, second $second_ip."
    exit 1
}

skip() { echo "  - $1 (skipped: $2)"; }

step() { echo; echo "$* ($(elapsed))"; }

elapsed() { local s=$(($(date +%s) - started)); printf '%dm%02ds' $((s / 60)) $((s % 60)); }

# moin is the CLI of this run. No call may hang the run.
moin() { timeout 900 "$work/moin" "$@"; }

# expect <what> <pattern> <command…>: the command works and prints the
# pattern (grep -E).
expect() {
    local what=$1 pattern=$2
    shift 2
    if ! out=$("$@" 2>&1); then
        fail "$what" "\`$*\` failed:" "$out"
    fi
    grep -qE -- "$pattern" <<<"$out" || fail "$what" "\`$*\` printed no /$pattern/:" "$out"
    pass "$what"
}

# said <what> <pattern>: the command of the last expect printed the
# pattern too.
said() {
    grep -qE -- "$2" <<<"$out" || fail "$1" "no /$2/ in:" "$out"
    pass "$1"
}

# refuse <what> <pattern> <command…>: the command prints nothing like the
# pattern.
refuse() {
    local what=$1 pattern=$2 out
    shift 2
    out=$("$@" 2>&1)
    grep -qE -- "$pattern" <<<"$out" && fail "$what" "\`$*\` printed /$pattern/:" "$out"
    pass "$what"
}

# eventually <seconds> <what> <pattern> <command…>: the command prints the
# pattern within that time; it is tried every 5 seconds.
eventually() {
    local seconds=$1 what=$2 pattern=$3 out until
    shift 3
    until=$(($(date +%s) + seconds))
    while :; do
        out=$("$@" 2>&1) && grep -qE -- "$pattern" <<<"$out" && break
        [ "$(date +%s)" -ge "$until" ] && fail "$what" "no /$pattern/ after ${seconds}s from \`$*\`:" "$out"
        sleep 5
    done
    pass "$what"
}

# get is a page over HTTPS, with a certificate that must be valid.
get() { curl -sS --max-time 20 "$@"; }

# rows is how many rows the table visits of shop has, read in its Postgres
# with moin exec.
rows() { moin exec shop --service db -- psql -U postgres -tAc 'SELECT count(*) FROM visits' | tr -d '[:space:]'; }

# json <python expression> reads JSON on stdin and prints the expression
# of it, d being the JSON.
json() { python3 -c "import json, sys; d = json.load(sys.stdin); print($1)"; }

# pty runs a command on a terminal of its own, as a person would.
pty() { python3 -c 'import pty, sys; sys.exit(pty.spawn(sys.argv[1:]) >> 8)' "$@"; }

# git_app makes a fresh git repository of a test app in the work folder.
git_app() {
    rm -rf "${work:?}/$1"
    cp -r "$here/$1" "$work/$1"
    git -C "$work/$1" init -q
    git -C "$work/$1" add .
    git -C "$work/$1" -c user.name=ada -c user.email=ada@example.com commit -qm v1
}

commit() { git -c user.name=ada -c user.email=ada@example.com commit -qam "$1"; }

# hooks are the alerts the webhook on second got.
hooks() { ssh second cat /root/hooks.log; }

# ---------------------------------------------------------------- second

# prepare_second sets up the second server: Docker, moin, SeaweedFS as the
# bucket, a webhook, and Caddy, which gives the watcher HTTPS. It runs in
# the background while prod deploys.
prepare_second() {
    set -e
    ssh second 'curl -fsSL https://get.docker.com | sh' >/dev/null
    ssh second 'cat > /usr/local/bin/moin && chmod 755 /usr/local/bin/moin' <"$server_moin"
    ssh second 'mkdir -p /root/s3 && cat > /root/s3/s3.json' <<EOF
{"identities":[{"name":"moin","credentials":[{"accessKey":"e2e-access","secretKey":"$bucket_secret"}],"actions":["Admin","Read","Write","List","Tagging"]}]}
EOF
    ssh second 'docker run -d --name bucket -p 8333:8333 -v /root/s3:/etc/s3 chrislusf/seaweedfs server -s3 -s3.config=/etc/s3/s3.json' >/dev/null
    local i
    for i in $(seq 40); do
        ssh second 'echo s3.bucket.create -name backups | docker exec -i bucket weed shell >/dev/null 2>&1; echo s3.bucket.list | docker exec -i bucket weed shell 2>/dev/null' | grep -q backups && break
        sleep 3
    done
    ssh second 'cat > /root/hook.py && systemd-run --unit e2e-hook python3 /root/hook.py 8099 /root/hooks.log && touch /root/hooks.log' <"$here/hook.py" >/dev/null 2>&1
    ssh second "docker run -d --name proxy --network host caddy:2 caddy reverse-proxy --from ${watcher_url#https://} --to 127.0.0.1:8080" >/dev/null
    echo "second is ready"
}

# ---------------------------------------------------------------- the run

step "Set up: prod $prod_ip, second $second_ip"
arch=$(ssh prod uname -m)
case $arch in
x86_64) arch=amd64 ;;
aarch64) arch=arm64 ;;
esac
server_moin=$work/moin
if [ "linux/$arch" = "$(cd "$repo" && go env GOOS)/$(cd "$repo" && go env GOARCH)" ]; then
    pass "the servers are linux/$arch, like this machine: the first deploy installs this moin"
else
    # A dev build installs only itself, so a server of another kind gets
    # its build, and its daemon, before the first deploy.
    server_moin=$work/moin-linux-$arch
    (cd "$repo" && GOOS=linux GOARCH=$arch go build -o "$server_moin" ./cmd/moin) || fail "build moin for linux/$arch"
    ssh prod 'cat > /usr/local/bin/moin && chmod 755 /usr/local/bin/moin' <"$server_moin"
    ssh prod 'curl -fsSL https://get.docker.com | sh >/dev/null 2>&1 && moin here setup prod' ||
        fail "set up moin on prod by hand"
    pass "the servers are linux/$arch: moin and its daemon are on prod before the first deploy"
fi
prepare_second >"$work/second.log" 2>&1 &
second_pid=$!

step "1. A first deploy of shop, with Postgres, from a fresh git repository"
git_app shop
cd "$work/shop" || exit 1
expect "moin deploy prod sets up moin and deploys" "shop is live: $shop_url" moin deploy prod -y
said "the deploy found Postgres and points at its backups" "found. See its backups"
eventually 180 "shop answers on its instant address, over HTTPS" "shop v1 .*rows=[0-9]+" get "$shop_url/"
expect "shop wrote a row in Postgres" "^[1-9][0-9]*$" rows

step "2. Everyday work on shop"
sed -i 's/"v1"/"v2"/' index.js && commit v2
expect "moin deploy, without a server, goes where shop runs" "shop is live" moin deploy
eventually 60 "v2 answers" "shop v2" get "$shop_url/"
expect "moin shows prod and shop" "shop" moin
expect "moin --json is JSON with shop" "shop" sh -c "'$work/moin' --json | python3 -m json.tool"
expect "moin apps --json shows shop on prod" '"prod"' moin apps --json
expect "moin logs shop shows the app's log" "visit /" moin logs shop --tail 50
expect "moin exec shop runs a command in the app" "^db$" moin exec shop -- printenv PGHOST
printf '#!/bin/sh\necho GREETING=hello-e2e >> "$1"\n' >"$work/editor.sh" && chmod +x "$work/editor.sh"
export EDITOR=$work/editor.sh
expect "moin env edit shop, in \$EDITOR on a terminal" "shop" pty "$work/moin" env edit shop
expect "moin env shop lists GREETING" "GREETING" moin env shop
refuse "moin env shop shows no value" "hello-e2e" moin env shop
eventually 60 "shop runs with the new env" "greeting=hello-e2e" get "$shop_url/"
expect "moin domains add shop" "shop.example.com" moin domains add shop shop.example.com
expect "moin domains shop lists it" "shop.example.com" moin domains shop --json
expect "moin domains rm shop" "no longer answers on shop.example.com" moin domains rm shop shop.example.com -y
refuse "the domain is gone" "shop.example.com" moin domains shop --json
expect "moin rollback shop --to v1" "shop" moin rollback shop --to v1
eventually 60 "v1 answers again" "shop v1" get "$shop_url/"

step "3. Backups into a bucket on second, restore, undo"
wait "$second_pid" || fail "second could not be set up" "$(cat "$work/second.log")"
pass "second runs SeaweedFS, a webhook and Caddy"
expect "moin bucket set prod" "prod backs up to backups" sh -c "echo '$bucket_secret' | '$work/moin' bucket set prod --endpoint http://$second_ip:8333 --bucket backups --access-key e2e-access"
refuse "moin bucket prod never shows the secret" "$bucket_secret" moin bucket prod --json
eventually 240 "the daemon took the first copy by itself" '"copies"' moin backups shop --json
copy=$(moin backup shop --json | json 'd[0]["copy"]')
[ -n "$copy" ] || fail "moin backup shop made a copy" "no copy in its --json"
pass "moin backup shop made the copy $copy"
before=$(rows)
for i in 1 2 3; do get "$shop_url/" >/dev/null; done
after=$(rows)
[ "$after" -eq $((before + 3)) ] || fail "three visits wrote three rows" "$before rows before, $after after"
pass "three visits wrote three rows: $before → $after"
expect "moin backups shop lists the copy" "$copy" moin backups shop
undo=$(moin restore shop --to "$copy" -y --json | json 'd["undo"]')
[ "$(rows)" = "$before" ] || fail "moin restore shop --to $copy" "$(rows) rows, the copy had $before"
pass "moin restore shop --to $copy: $before rows, as in the copy"
eventually 60 "shop answers after the restore" "rows=" get "$shop_url/count"
expect "moin restore shop --to $undo" "is back at" moin restore shop --to "$undo" -y
[ "$(rows)" = "$after" ] || fail "the undo brought the rows back" "$(rows) rows, before the restore there were $after"
pass "the undo brought the rows back: $after"
skip "copies are encrypted with age, and the key restores them" "encryption is being built"

step "4. A watcher on second"
ssh second "mkdir -p /etc/moin && umask 077 && cat > /etc/moin/watcher.yml" <<EOF
password: $watcher_password
alerts:
  - kind: webhook
    url: http://127.0.0.1:8099/hook
EOF
ssh second 'cat > /etc/systemd/system/moin-watcher.service && systemctl daemon-reload && systemctl start moin-watcher' <<'EOF'
[Service]
ExecStart=/usr/local/bin/moin watcher run --listen 127.0.0.1:8080
Restart=always
EOF
eventually 120 "moin watcher run answers over HTTPS" "password" get "$watcher_url/"
line=$(ssh second moin watcher token prod --url "$watcher_url" | grep 'watcher connect prod')
token=${line##* }
[ -n "$token" ] || fail "moin watcher token prod prints the connect line" "$line"
pass "moin watcher token prod prints: ${line/$token/<token>}"
ssh second "printf 'servers:\n  prod: %s\n' $token >> /etc/moin/watcher.yml && systemctl restart moin-watcher"
expect "moin watcher connect prod" "prod reports to $watcher_url" moin watcher connect prod "$watcher_url" "$token"
expect "moin servers shows the watcher" "${watcher_url#https://}" moin servers
eventually 150 "a report arrived: the page shows prod and shop" "shop" get -u "e2e:$watcher_password" "$watcher_url/"
ssh second 'truncate -s 0 /root/hooks.log'
ssh prod systemctl stop moin-daemon
pass "prod's daemon stopped"
eventually 330 "the watcher alerted the silence to the webhook" "prod stopped reporting" hooks
ssh prod systemctl start moin-daemon
eventually 150 "and that prod reports again" "prod reports again" hooks

step "5. An app of the catalog"
cd "$work" || exit 1
expect "moin deploy prod --catalog it-tools" "it-tools is live" moin deploy prod --catalog it-tools -y
eventually 120 "it-tools answers over HTTPS" "<html|<!DOCTYPE" get "$tools_url/"
cd "$work/shop" || exit 1

step "6. The team: keys, and a sudo user"
ssh-keygen -q -t ed25519 -N '' -C anna@e2e -f "$work/anna"
anna() { ssh -i "$work/anna" -o IdentitiesOnly=yes -o IdentityAgent=none prod true; }
expect "moin keys add prod anna.pub" "anna@e2e may log in to prod" moin keys add prod "$work/anna.pub"
expect "moin keys prod lists anna" "anna@e2e" moin keys prod --json
anna || fail "anna logs in with her key"
pass "anna logs in with her key"
expect "moin keys rm prod anna@e2e" "anna@e2e can no longer log in" moin keys rm prod anna@e2e -y
anna 2>/dev/null && fail "anna is out again"
pass "anna is out again"
ssh prod 'useradd -m -s /bin/bash ada && mkdir -p /home/ada/.ssh && cp /root/.ssh/authorized_keys /home/ada/.ssh/ && chown -R ada: /home/ada/.ssh && echo "ada ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/ada'
git_app hello
cd "$work/hello" || exit 1
expect "ada, a sudo user, deploys hello: moin deploy prod-ada" "hello is live" moin deploy prod-ada -y
eventually 120 "hello answers over HTTPS" "hello from a sudo user" get "$hello_url/"
expect "moin servers rm prod-ada" "forgot prod-ada" moin servers rm prod-ada

step "Coming, and added here when they land"
skip "moin secure prod, then moin unlock from the console" "being built"
skip "public SSH closed after the first deploy" "being built"
skip "moin follow prod shop --branch main" "being built"
skip "a write-only copy at moin cloud" "being built"

echo
echo "All steps passed in $(elapsed)."
