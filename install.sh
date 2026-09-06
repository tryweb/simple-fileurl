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
  [ -n "${RELEASE_REF:-}" ] || { printf 'install: error: set RELEASE_REF to an immutable release tag when using a remote installer\n' >&2; exit 1; }
fi

RELEASE_REF="${RELEASE_REF:-main}"
REPO_URL="${REPO_URL:-https://raw.githubusercontent.com/tryweb/simple-fileurl/$RELEASE_REF}"
CHECKSUMS_FILE=

if [ "$PINNED_REF_REQUIRED" -eq 1 ]; then
  printf '%s\n' "$RELEASE_REF" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$|^sha-[0-9a-fA-F]{7,64}$' \
    || { printf 'install: error: RELEASE_REF must be an immutable release tag\n' >&2; exit 1; }
fi

cleanup() { [ -z "$CHECKSUMS_FILE" ] || rm -f "$CHECKSUMS_FILE"; }
trap cleanup EXIT

fail() { printf 'install: error: %s\n' "$*" >&2; exit 1; }
info() { printf 'install: %s\n' "$*"; }

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

require_tty() {
  [ -r /dev/tty ] || fail 'interactive terminal required; pre-create .env or run with ssh -t'
}

required_values_present() {
  [ -n "$(env_value HOST_SHARE_PATH)" ] &&
    [ -n "$(env_value PUBLIC_URL)" ] &&
    [ -n "$(env_value SFTP_ADMIN_PASSWORD)" ] &&
    [ -n "$(env_value ADMIN_TOKEN)" ] &&
    [ -n "$(env_value IMAGE_TAG)" ]
}

validate_release_values() {
  local image_tag admin_token
  image_tag=$(env_value IMAGE_TAG)
  admin_token=$(env_value ADMIN_TOKEN)
  printf '%s\n' "$image_tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$|^sha-[0-9a-fA-F]{7,64}$' \
    || fail 'IMAGE_TAG must be an immutable vX.Y.Z or sha-<hex> release tag'
  [ -n "$admin_token" ] \
    || fail 'ADMIN_TOKEN must not be empty'
}

require_docker() {
  command -v docker >/dev/null 2>&1 || fail 'Docker is not installed'
  docker compose version >/dev/null 2>&1 || fail 'Docker Compose v2 is required'
  docker info >/dev/null 2>&1 || fail 'Docker daemon is not reachable'
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

env_value() {
  local key=$1 default=${2:-}
  awk -F= -v key="$key" -v default="$default" '$1 == key {sub(/^[^=]*=/, ""); print ($0 == "" ? default : $0); found=1; exit} END {if (!found) print default}' .env
}

prompt_value() {
  local key=$1 prompt=$2 default=${3:-} value
  value=$(env_value "$key" "$default")
  if [ -n "$value" ] && [ "$value" != '<required>' ]; then
    return
  fi
  if [ -n "$default" ]; then
    read -r -p "$prompt [$default]: " value < /dev/tty
    value=${value:-$default}
  else
    read -r -p "$prompt: " value < /dev/tty
  fi
  [ -n "$value" ] || fail "$key cannot be empty"
  set_env_value "$key" "$value"
}

prompt_secret() {
  local key=$1 prompt=$2 value
  value=$(env_value "$key")
  if [ -n "$value" ] && [ "$value" != '<required>' ]; then
    return
  fi
  read -r -s -p "$prompt: " value < /dev/tty
  printf '\n'
  [ -n "$value" ] || fail "$key cannot be empty"
  set_env_value "$key" "$value"
}

prepare_share() {
  local share prefix gid resolved prefix_root segment
  local -a segments
  share=$(env_value HOST_SHARE_PATH)
  prefix=$(env_value SHARE_PREFIX files)
  gid=$(env_value SFTP_GID 2000)
  [ -n "$share" ] && [ "$share" != '<required>' ] || fail 'HOST_SHARE_PATH cannot be empty'
  case "$share" in
    /*) ;;
    *) fail 'HOST_SHARE_PATH must be absolute' ;;
  esac
  case "$share" in
    /|*'/../'*|*/..|*'/./'*|*/.) fail 'HOST_SHARE_PATH contains an unsafe path segment' ;;
  esac
  case "$prefix" in
    ''|/*|*'//'*) fail 'SHARE_PREFIX contains an unsafe path segment' ;;
  esac
  IFS=/ read -r -a segments <<< "$prefix"
  for segment in "${segments[@]}"; do
    printf '%s\n' "$segment" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._~-]*$' \
      || fail 'SHARE_PREFIX contains invalid characters'
  done
  printf '%s\n' "$gid" | grep -Eq '^[0-9]+$' || fail 'SFTP_GID must be numeric'
  command -v realpath >/dev/null 2>&1 || fail 'realpath is required to validate the host share path'
  if [ "$(id -u)" -eq 0 ]; then
    mkdir -p "$share"
  elif command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "$share"
  else
    fail "prepare $share as root:root with sudo or run the installer as root"
  fi
  resolved=$(realpath "$share")
  [ "$resolved" != / ] || fail 'HOST_SHARE_PATH cannot resolve to /'
  if [ "$(id -u)" -eq 0 ]; then
    mkdir -p "$share/$prefix"
  else
    sudo mkdir -p "$share/$prefix"
  fi
  prefix_root=$(realpath "$share/$prefix")
  case "$prefix_root" in
    "$resolved"/*) ;;
    *) fail 'SHARE_PREFIX resolves outside HOST_SHARE_PATH' ;;
  esac
  if [ "$(id -u)" -eq 0 ]; then
    chown root:root "$share"
    chmod 0755 "$share"
    chown "root:$gid" "$share/$prefix"
    chmod 2775 "$share/$prefix"
  elif command -v sudo >/dev/null 2>&1; then
    sudo chown root:root "$share"
    sudo chmod 0755 "$share"
    sudo chown "root:$gid" "$share/$prefix"
    sudo chmod 2775 "$share/$prefix"
  else
    fail "prepare $share as root:root with sudo or run the installer as root"
  fi
}

start_services() {
  docker compose pull
  docker compose up -d --remove-orphans
  docker compose ps
  docker compose ps --status running --services | grep -qx 'file-sharing' || fail 'file-sharing did not start'
}

main() {
  if [ -f docker-compose.yml ] && [ -f .env ]; then
    if [ -f ./upgrade.sh ]; then
      exec ./upgrade.sh "$@"
    fi
    load_checksums
    download_verified upgrade.sh ./upgrade.sh
    chmod 0755 ./upgrade.sh
    exec ./upgrade.sh "$@"
  fi

  require_docker
  load_checksums
  [ -f .env.example ] || download_verified .env.example .env.example
  [ -f docker-compose.yml ] || download_verified docker-compose.yml docker-compose.yml
  [ -f .env ] || cp .env.example .env
  chmod 0600 .env

  required_values_present || require_tty
  prompt_value HOST_SHARE_PATH 'Host share directory'
  prompt_value SHARE_PREFIX 'SFTP logical prefix' files
  prompt_value PUBLIC_URL 'Public base URL'
  prompt_value IMAGE_TAG 'Release image tag'
  prompt_secret SFTP_ADMIN_PASSWORD 'SFTP Admin password'
  prompt_secret ADMIN_TOKEN 'Share-link API token'
  validate_release_values
  prepare_share
  docker compose config >/dev/null
  start_services
  info "Web health: $(env_value PUBLIC_URL)/healthz"
  info "Admin UI: http://<host>:$(env_value SFTP_ADMIN_PORT 8081) (remote-accessible by default; protect with TLS/private network/firewall)"
  info "SFTP port: $(env_value SFTP_PORT 2222)"
}

main "$@"
