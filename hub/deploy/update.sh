#!/bin/sh
# Starts the moin hub, or updates it to the newest version. Run it again
# for every update. MOIN_IMAGE picks another image, such as a version tag.
set -eu
cd "$(dirname "$0")"

# The new version. The running hub keeps serving.
docker compose pull

# The new version proves itself on a copy of the database, with these
# settings. If it fails, this stops here and the running hub stays.
# traefik.enable=false: no visitor ever reaches this one.
docker compose run --rm --no-deps -T -l traefik.enable=false hub ./main preflight

# Only now switch.
docker compose up -d --remove-orphans
