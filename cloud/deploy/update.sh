#!/bin/sh
# Starts the site, or updates it to the newest image. The Deploy workflow
# runs it on the server. MOIN_IMAGE picks another image, such as a version.
set -eu
cd "$(dirname "$0")"
docker compose pull
# The new version checks the settings first, while the running site stays.
# A missing or wrong setting ends it here.
docker compose run --rm --no-deps -T cloud ./site preflight
# --wait returns once everything runs healthy, and gives up after two
# minutes, such as for a tunnel whose token no longer works.
docker compose up -d --remove-orphans --wait --wait-timeout 120
