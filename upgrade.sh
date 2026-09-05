#!/usr/bin/env bash
set -euo pipefail
umask 077

if [ -f "${BASH_SOURCE[0]:-}" ]; then
  SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
else
  SCRIPT_DIR=$PWD
fi
cd "$SCRIPT_DIR"

PINNED_REF_REQUIRED=0
if [ ! -f "${BASH_SOURCE[0]:-}" ] && [ -z "${REPO_URL:-}" ]; then
  PINNED_REF_REQUIRED=1
  [ -n "${RELEASE_REF:-}" ] || { printf 'upgrade: error: set RELEASE_REF to an immutable release tag when using a remote upgrader\n' >&2; exit 1; }
fi

RELEASE_REF="${RELEASE_REF:-${1:-}}"
if [ -z "$RELEASE_REF" ] && [ -f .env ]; then
  RELEASE_REF=$(awk -F= '$1 == "IMAGE_TAG" {print $2; exit}' .env)
fi
RELEASE_REF="${RELEASE_REF:-main}"
REPO_URL="${REPO_URL:-https://raw.githubusercontent.com/tryweb/simple-fileurl/$RELEASE_REF}"
BACKUP_RETENTION="${BACKUP_RETENTION:-5}"
CHECKSUMS_FILE=

if [ "$PINNED_REF_REQUIRED" -eq 1 ]; then
  printf '%s\n' "$RELEASE_REF" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$|^sha-[0-9a-fA-F]{7,64}$' \
    || { printf 'upgrade: error: RELEASE_REF must be an immutable release tag\n' >&2; exit 1; }
fi

cleanup() { [ -z "$CHECKSUMS_FILE" ] || rm -f "$CHECKSUMS_FILE"; }
trap cleanup EXIT
case "$BACKUP_RETENTION" in
  ''|0*|*[!0-9]*) BACKUP_RETENTION=5 ;;
esac

fail() { printf 'upgrade: error: %s\n' "$*" >&2; exit 1; }
info() { printf 'upgrade: %s\n' "$*"; }

download() {
  local url=$1 output=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$output"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$output" "$url"
  else
    fail 'curl or wget is required'
  fi
  [ -s "$output" ] || fail "download was empty: $url"
}

checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail 'sha256sum or shasum is required to verify release artifacts'
  fi
}

load_checksums() {
  CHECKSUMS_FILE=$(mktemp)
  download "$REPO_URL/artifact-checksums.txt" "$CHECKSUMS_FILE"
}

download_verified() {
  local artifact=$1 output=$2 expected actual
  download "$REPO_URL/$artifact" "$output"
  expected=$(awk -v artifact="$artifact" '$2 == artifact {print $1; found=1; exit} END {if (!found) exit 1}' "$CHECKSUMS_FILE") \
    || fail "checksum missing for $artifact"
  actual=$(checksum "$output")
  [ "$actual" = "$expected" ] || fail "checksum mismatch for $artifact"
}

require_installed() {
  [ -f docker-compose.yml ] || fail 'docker-compose.yml is missing; run install.sh first'
  [ -f .env ] || fail '.env is missing; run install.sh first'
  command -v docker >/dev/null 2>&1 || fail 'Docker is not installed'
  docker compose version >/dev/null 2>&1 || fail 'Docker Compose v2 is required'
  docker info >/dev/null 2>&1 || fail 'Docker daemon is not reachable'
  chmod 0600 .env
}

env_value() {
  local key=$1 default=${2:-}
  awk -F= -v key="$key" -v default="$default" '$1 == key {sub(/^[^=]*=/, ""); print ($0 == "" ? default : $0); found=1; exit} END {if (!found) print default}' .env
}

set_env_value() {
  local key=$1 value=$2 tmp
  tmp=$(mktemp)
  awk -v key="$key" -v value="$value" '
    $0 ~ "^" key "=" { print key "=" value; found=1; next }
    { print }
    END { if (!found) print key "=" value }
  ' .env >"$tmp"
  mv "$tmp" .env
  chmod 0600 .env
}

backup_files() {
  local stamp backup count index suffix=0
  local -a backups
  stamp=$(date +%Y%m%d_%H%M%S)
  backup="backups/upgrade_${stamp}"
  while [ -e "$backup" ]; do
    suffix=$((suffix + 1))
    backup="backups/upgrade_${stamp}_$(printf '%02d' "$suffix")"
  done
  mkdir -p -m 700 backups
  chmod 700 backups
  mkdir -m 700 "$backup"
  cp docker-compose.yml .env "$backup/"
  chmod 600 "$backup/.env" docker-compose.yml .env
  info "backup created: $backup"
  shopt -s nullglob
  backups=(backups/upgrade_*)
  shopt -u nullglob
  count=${#backups[@]}
  if [ "$count" -gt "$BACKUP_RETENTION" ]; then
    for ((index = 0; index < count - BACKUP_RETENTION; index++)); do
      rm -rf -- "${backups[index]}"
    done
  fi
}

merge_env_defaults() {
  local example tmp key
  example=$(mktemp)
  download_verified .env.example "$example"
  while IFS= read -r key; do
    grep -qE "^${key}=" .env || {
      tmp=$(grep -E "^${key}=" "$example" | head -n1)
      printf '\n%s\n' "$tmp" >> .env
    }
  done < <(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$example" | cut -d= -f1)
  rm -f "$example"
}

update_compose() {
  local tmp
  tmp=$(mktemp)
  download_verified docker-compose.yml "$tmp"
  docker compose --project-directory "$SCRIPT_DIR" --env-file "$SCRIPT_DIR/.env" -f "$tmp" config >/dev/null \
    || fail 'downloaded docker-compose.yml failed validation'
  mv "$tmp" docker-compose.yml
}

wait_for_services() {
  local attempt
  for attempt in $(seq 1 30); do
    if docker compose ps --status running --services | grep -qx 'file-sharing' && \
      curl -fsS "http://127.0.0.1:$(env_value PORT 8080)/healthz" >/dev/null; then
      return 0
    fi
    sleep 2
  done
  docker compose ps
  docker compose logs --tail=80
  fail 'services did not become healthy'
}

main() {
  require_installed
  load_checksums
  backup_files
  merge_env_defaults
  if [ "${1:-}" != '' ]; then
    set_env_value IMAGE_TAG "$1"
  fi
  printf '%s\n' "$(env_value IMAGE_TAG)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$|^sha-[0-9a-fA-F]{7,64}$' \
    || fail 'IMAGE_TAG must be an immutable vX.Y.Z or sha-<hex> release tag'
  update_compose
  docker compose config >/dev/null
  docker compose pull
  docker compose up -d --force-recreate --remove-orphans
  wait_for_services
  docker compose ps
  info 'upgrade complete; rollback from a timestamped backup with: cp backups/upgrade_<timestamp>/{.env,docker-compose.yml} . && docker compose up -d'
}

main "$@"
