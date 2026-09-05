#!/bin/sh
# SFTP container entrypoint: validate the chroot/share contract, reconcile
# users without restarting sshd, then run sshd in the foreground.
#
# Layout (volumes are mounted by compose; this script never re-mounts):
#   /share                     chroot root, must be root-owned, non-writable
#   /share/${SHARE_PREFIX}     the only writable subtree (group SFTP_GID)
#   /var/lib/sftp-users/       shared manifest volume (read-only mount here)
#
# Required env: SHARE_PREFIX (single path segment).
# Optional env: SFTP_GID (default 2000), WEB_GID (default 2001),
# SFTP_POLL_INTERVAL (default 2).
#
# Test overrides: SFTP_SHARE_ROOT, SFTP_USERS_FILE, SFTP_AUTH_KEYS_DIR,
# SFTP_SSHD_CONFIG, SFTP_SKIP_USERADD. `entrypoint.sh check` only validates.
set -eu

SHARE_ROOT="${SFTP_SHARE_ROOT:-/share}"
SHARE_PREFIX="${SHARE_PREFIX:?SHARE_PREFIX is required}"
SFTP_GID="${SFTP_GID:-2000}"
SFTP_GROUP="${SFTP_GROUP:-sftpusers}"
WEB_GID="${WEB_GID:-2001}"
WEB_GROUP="${WEB_GROUP:-webreaders}"
USERS_FILE="${SFTP_USERS_FILE:-/var/lib/sftp-users/users.json}"
AUTH_KEYS_DIR="${SFTP_AUTH_KEYS_DIR:-/etc/ssh/authorized_keys.d}"
SSHD_CONFIG="${SFTP_SSHD_CONFIG:-/etc/ssh/sshd_config}"
POLL_INTERVAL="${SFTP_POLL_INTERVAL:-2}"
SKIP_USERADD="${SFTP_SKIP_USERADD:-0}"
RECONCILE_BIN="${SFTP_RECONCILE_BIN:-/usr/local/bin/reconcile.sh}"

fail() { printf 'entrypoint: error: %s\n' "$*" >&2; exit 1; }
log() { printf 'entrypoint: %s\n' "$*"; }

valid_gid() {
  case "$1" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "$1" -ge 1 ] && [ "$1" -le 4294967295 ] 2>/dev/null
}

ensure_group() {
  if [ "$SKIP_USERADD" = "1" ]; then return 0; fi
  if getent group "$SFTP_GROUP" >/dev/null 2>&1; then
    existing="$(getent group "$SFTP_GROUP" | cut -d: -f3)"
    [ "$existing" = "$SFTP_GID" ] || fail "group $SFTP_GROUP has GID $existing, want $SFTP_GID"
  else
    addgroup -g "$SFTP_GID" "$SFTP_GROUP" >/dev/null 2>&1 \
      || fail "cannot create group $SFTP_GROUP with GID $SFTP_GID"
  fi
}

ensure_web_group() {
  # Creates the web readers group owning per-user directories, mirroring
  # ensure_group for SFTP_GID. Runs before the reconcile loop so per-user
  # directory creation in reconcile.sh can chown to WEB_GID.
  if [ "$SKIP_USERADD" = "1" ]; then return 0; fi
  if getent group "$WEB_GROUP" >/dev/null 2>&1; then
    existing="$(getent group "$WEB_GROUP" | cut -d: -f3)"
    [ "$existing" = "$WEB_GID" ] || fail "group $WEB_GROUP has GID $existing, want $WEB_GID"
  else
    addgroup -g "$WEB_GID" "$WEB_GROUP" >/dev/null 2>&1 \
      || fail "cannot create group $WEB_GROUP with GID $WEB_GID"
  fi
}

validate_share() {
  case "$SHARE_PREFIX" in
    ''|*/*|.|..) fail "SHARE_PREFIX must be a single path segment, got '$SHARE_PREFIX'" ;;
  esac
  printf '%s' "$SHARE_PREFIX" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._-]*$' 2>/dev/null \
    || fail "SHARE_PREFIX must match ^[A-Za-z0-9][A-Za-z0-9._-]*\$, got '$SHARE_PREFIX'"
  valid_gid "$SFTP_GID" || fail "SFTP_GID must be a numeric GID, got '$SFTP_GID'"
  valid_gid "$WEB_GID" || fail "WEB_GID must be a numeric GID, got '$WEB_GID'"
  [ "$WEB_GID" != "$SFTP_GID" ] || fail "WEB_GID ($WEB_GID) must differ from SFTP_GID ($SFTP_GID)"

  [ -d "$SHARE_ROOT" ] || fail "$SHARE_ROOT is missing or not a directory"
  owner="$(stat -c '%u %g %a' "$SHARE_ROOT")"
  set -- $owner
  [ "$1" = "0" ] && [ "$2" = "0" ] || fail "$SHARE_ROOT must be root-owned, got uid=$1 gid=$2"
  # Last-three mode digits must have no group/other write bits (chroot
  # requires a non-writable root; never chmod the mounted share here).
  mode="$3"
  case "${#mode}" in
    4) mode="${mode#?}" ;;
  esac
  grp="${mode%?}"; grp="${grp#?}"
  oth="${mode#??}"
  case "$grp$oth" in
    *[2367]*|*[2367]) fail "$SHARE_ROOT must not be writable by group/other, got mode $3" ;;
  esac

  prefix="$SHARE_ROOT/$SHARE_PREFIX"
  [ -d "$prefix" ] || fail "$prefix is missing or not a directory"
  pgid="$(stat -c '%g %a' "$prefix")"
  set -- $pgid
  [ "$1" = "$SFTP_GID" ] || fail "$prefix must be group $SFTP_GID, got gid $1"
  pmode="$2"
  case "${#pmode}" in
    4) pmode="${pmode#?}" ;;
  esac
  g="${pmode%?}"; g="${g#?}"
  case "$g" in
    3|7) ;;
    *) fail "$prefix must be group-writable and traversable (g+wx), got mode $2" ;;
  esac
  case "$2" in
    [2367]???) ;;
    *) log "warning: $prefix lacks setgid; new files may miss group $SFTP_GID" ;;
  esac
}

prepare_ssh() {
  mkdir -p "$AUTH_KEYS_DIR"
  if [ "$(id -u)" = "0" ]; then
    chown root:root "$AUTH_KEYS_DIR"
    chmod 0755 "$AUTH_KEYS_DIR"
  fi
  if [ "$(id -u)" = "0" ]; then
    ssh-keygen -A >/dev/null 2>&1 || fail "ssh-keygen -A failed"
  fi
  [ -f "$SSHD_CONFIG" ] || fail "missing $SSHD_CONFIG"
  if command -v sshd >/dev/null 2>&1; then
    sshd -t -f "$SSHD_CONFIG" || fail "sshd -t rejected $SSHD_CONFIG"
  fi
}

reconcile_loop() {
  last=""
  while true; do
    cur="missing"
    if [ -f "$USERS_FILE" ]; then
      if command -v sha256sum >/dev/null 2>&1; then
        cur="$(sha256sum "$USERS_FILE" 2>/dev/null || echo unreadable)"
      else
        cur="$(stat -c '%s %Y' "$USERS_FILE" 2>/dev/null || echo unreadable)"
      fi
    fi
    if [ "$cur" != "$last" ]; then
      "$RECONCILE_BIN" || log "reconcile failed; keeping previous state"
      last="$cur"
    fi
    sleep "$POLL_INTERVAL"
  done
}

if [ "${1:-}" = "check" ]; then
  validate_share
  log "share contract OK: root=$SHARE_ROOT prefix=$SHARE_PREFIX gid=$SFTP_GID"
  exit 0
fi

validate_share
ensure_group
ensure_web_group
prepare_ssh
"$RECONCILE_BIN" || log "initial reconcile failed; sshd still starts with previous state"

reconcile_loop &
# shellcheck disable=SC2086
exec /usr/sbin/sshd -D -e -f "$SSHD_CONFIG"
