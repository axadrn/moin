#!/bin/sh
# Starts the moin hub, or updates it to the newest version. Run it again
# for every update. MOIN_IMAGE picks another image, such as a version tag.
set -eu
cd "$(dirname "$0")"

# The new version. The running hub keeps serving.
docker compose pull

# The new version starts once, on a copy of the database, with these
# settings. If it fails, this stops here and the running hub stays. Caddy
# and cloudflared never reach it: a run container does not get the name
# hub. traefik.enable=false keeps Traefik away from it.
docker compose run --rm --no-deps -T -l traefik.enable=false hub ./main preflight

# Only now switch. --wait returns once the new hub is healthy.
docker compose up -d --remove-orphans --wait
