#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

bash -n "$ROOT/install.sh"
bash -n "$ROOT/upgrade.sh"
grep -q 'exec ./upgrade.sh' "$ROOT/install.sh"
grep -q '\[ -f docker-compose.yml \] && \[ -f \.env \]' "$ROOT/install.sh"
grep -q 'download_verified' "$ROOT/install.sh"
grep -q 'docker compose pull' "$ROOT/upgrade.sh"
grep -q 'docker compose up -d --force-recreate' "$ROOT/upgrade.sh"
grep -q 'downloaded docker-compose.yml failed validation' "$ROOT/upgrade.sh"
grep -q 'umask 077' "$ROOT/install.sh"
grep -q 'umask 077' "$ROOT/upgrade.sh"
grep -q 'chmod 0600' "$ROOT/upgrade.sh"
grep -q 'backup_' "$ROOT/upgrade.sh"
grep -q 'SCRIPT_DIR=\$PWD' "$ROOT/install.sh"
grep -q 'SCRIPT_DIR=\$PWD' "$ROOT/upgrade.sh"
test -s "$ROOT/artifact-checksums.txt"
echo 'INSTALL/UPGRADE SCRIPT OK'
