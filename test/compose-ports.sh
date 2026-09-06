#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fail() {
  echo "COMPOSE PORTS FAIL: $1" >&2
  exit 1
}

service_block() {
  service=$1
  awk -v service="$service" '
    $0 == "  " service ":" { found=1 }
    found && $0 != "  " service ":" && /^  [^ ]/ { exit }
    found { print }
  '
}

prod_config=$(env \
  HOST_SHARE_PATH=/tmp/simple-fileurl-share \
  SHARE_PREFIX=files \
  PUBLIC_URL=https://example.test \
  ADMIN_TOKEN=test-admin-token \
  SFTP_ADMIN_PASSWORD=test-admin-password \
  IMAGE_TAG=sha-0123456789abcdef \
  docker compose -f "$ROOT/docker-compose.yml" config)
prod_admin=$(printf '%s\n' "$prod_config" | service_block sftp-admin)
[ "$(printf '%s\n' "$prod_admin" | grep -c 'published: "8081"')" -eq 1 ] \
  || fail 'production Admin port is not published exactly once'
printf '%s\n' "$prod_admin" | grep -q 'host_ip:' \
  && fail 'production Admin port is unexpectedly localhost-only'

dev_config=$(docker compose -f "$ROOT/docker-compose.dev.yml" config)
dev_admin=$(printf '%s\n' "$dev_config" | service_block sftp-admin)
[ "$(printf '%s\n' "$dev_admin" | grep -c 'published: "18081"')" -eq 1 ] \
  || fail 'dev Admin port is not published exactly once'
printf '%s\n' "$dev_admin" | grep -q 'host_ip:' \
  && fail 'dev Admin port is unexpectedly localhost-only'

override="$TMP/docker-compose.override.yml"
cat >"$override" <<'EOF'
services:
  sftp-admin:
    ports: !override
      - "127.0.0.1:${SFTP_ADMIN_PORT:-18081}:8080"
EOF
rollback_config=$(docker compose \
  -f "$ROOT/docker-compose.dev.yml" \
  -f "$override" config)
rollback_admin=$(printf '%s\n' "$rollback_config" | service_block sftp-admin)
[ "$(printf '%s\n' "$rollback_admin" | grep -c 'published: "18081"')" -eq 1 ] \
  || fail 'rollback Admin port is not published exactly once'
[ "$(printf '%s\n' "$rollback_admin" | grep -c 'host_ip: 127.0.0.1')" -eq 1 ] \
  || fail 'rollback Admin port is not localhost-only'
[ "$(printf '%s\n' "$rollback_admin" | grep -c 'target: 8080')" -eq 1 ] \
  || fail 'rollback override left duplicate Admin port entries'

echo 'COMPOSE PORTS OK'
