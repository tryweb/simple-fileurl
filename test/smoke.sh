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
ADMIN_TOKEN="${ADMIN_TOKEN:-dev-admin-token}"

fail() { echo "SMOKE FAIL: $1" >&2; exit 1; }

echo "BASE=${BASE} LINK_PREFIX=${LINK_PREFIX}"

health=$(curl -sf "${BASE}/healthz") || fail "unreachable /healthz"
[ "${health}" = '{"status":"ok"}' ] || fail "bad /healthz body: ${health}"
echo "ok: /healthz"

root_body=$(curl -s "${BASE}/") || fail "unreachable /"
[ -z "${root_body}" ] || fail "/ reveals content: ${root_body}"
echo "ok: / returns empty body"

created=$(curl -sf -X POST "${BASE}/api/links" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"scope":{"type":"admin"}}') || fail "POST /api/links failed"
linkid=$(printf '%s' "${created}" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "${linkid}" ] || fail "no link id in create response"
echo "ok: created link ${linkid}"

page=$(curl -sf "${BASE}/l/${linkid}") || fail "unreachable /l/${linkid}"
link=$(printf '%s' "${page}" | grep -o "${LINK_PREFIX//\//\/}/[0-9a-f]*/[0-9a-f]*" | head -1)
[ -n "${link}" ] || fail "no share link with prefix ${LINK_PREFIX} in page"
echo "ok: link page ${link}"

pwcreated=$(curl -sf -X POST "${BASE}/api/links" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"password":"smoke-pw","scope":{"type":"admin"}}') || fail "password link create failed"
pwid=$(printf '%s' "${pwcreated}" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "${pwid}" ] || fail "no password link id in create response"
code=$(curl -s -o /dev/null -w '%{http_code}' "${BASE}/l/${pwid}")
[ "${code}" = "401" ] || fail "password link page: want 401 got ${code}"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${BASE}/l/${pwid}/auth" --data-urlencode "password=wrong")
[ "${code}" = "401" ] || fail "wrong password: want 401 got ${code}"
auth_resp=$(curl -s -i -X POST "${BASE}/l/${pwid}/auth" --data-urlencode "password=smoke-pw")
code=$(printf '%s' "${auth_resp}" | grep -E '^HTTP/' | tail -1 | awk '{print $2}')
[ "${code}" = "302" ] || fail "auth: want 302 got ${code}"
session=$(printf '%s' "${auth_resp}" | grep -i '^set-cookie:' | head -1 | sed 's/^[^:]*:[[:space:]]*//;s/;.*//')
[ -n "${session}" ] || fail "no session cookie in auth response"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "${session}" "${BASE}/l/${pwid}")
[ "${code}" = "200" ] || fail "authed page: want 200 got ${code}"
echo "ok: password link flow"

lpath="${link#"${LINK_PREFIX}"}"
body=$(curl -sf "${BASE}${lpath}") || fail "download ${lpath} failed"
[ -n "${body}" ] || fail "empty download body"
echo "ok: download body: ${body}"

code=$(curl -s -o /dev/null -w '%{http_code}' "${BASE}/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
[ "${code}" = "404" ] || fail "unknown hash: want 404 got ${code}"
echo "ok: unknown hash -> 404"

echo "SMOKE OK"
