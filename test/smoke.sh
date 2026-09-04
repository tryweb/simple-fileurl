#!/bin/bash
# Smoke test for a running file-sharing service.
# The service must already be up (docker compose up or CI).
#
# Usage:
#   PORT=18080 test/smoke.sh
#   BASE=http://172.23.0.1:18080 LINK_PREFIX=https://weurl.everplast.net test/smoke.sh
set -uo pipefail

PORT="${PORT:-18080}"
BASE="${BASE:-http://localhost:${PORT}}"
LINK_PREFIX="${LINK_PREFIX:-${BASE}}"

fail() { echo "SMOKE FAIL: $1" >&2; exit 1; }

echo "BASE=${BASE} LINK_PREFIX=${LINK_PREFIX}"

health=$(curl -sf "${BASE}/healthz") || fail "unreachable /healthz"
[ "${health}" = '{"status":"ok"}' ] || fail "bad /healthz body: ${health}"
echo "ok: /healthz"

page=$(curl -sf "${BASE}/") || fail "unreachable /"
link=$(printf '%s' "${page}" | grep -o "${LINK_PREFIX//\//\/}/[0-9a-f]*/[0-9a-f]*" | head -1)
[ -n "${link}" ] || fail "no share link with prefix ${LINK_PREFIX} in index"
echo "ok: index link ${link}"

lpath="${link#"${LINK_PREFIX}"}"
body=$(curl -sf "${BASE}${lpath}") || fail "download ${lpath} failed"
[ -n "${body}" ] || fail "empty download body"
echo "ok: download body: ${body}"

code=$(curl -s -o /dev/null -w '%{http_code}' "${BASE}/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
[ "${code}" = "404" ] || fail "unknown hash: want 404 got ${code}"
echo "ok: unknown hash -> 404"

echo "SMOKE OK"
