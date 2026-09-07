#!/bin/sh
# Reconcile SFTP users from the shared manifest into local Linux accounts
# and per-user authorized-key files. One-shot: exit 0 when the effective
# state matches the manifest, non-zero when anything failed.
#
# Manifest schema (atomically written by the admin container):
#   {"version":1,"users":[{"username":"alice","enabled":true,
#     "authorized_keys":["ssh-ed25519 AAAA... alice"]}]}
#
# Guarantees:
# - Invalid manifests change nothing (previous valid key state is kept).
# - Per-user failures leave that user's existing key file untouched.
# - Disabled users, users absent from the manifest, and enabled users with
#   a valid empty authorized_keys list have no key file: an explicit []
#   revokes stale access with exit 0, while a missing field, a wrong type,
#   or an invalid entry preserves the previous key file with non-zero exit.
# - Key files are written atomically (tmp + rename), root-owned, 0640
#   (root:sftpusers; the most restrictive mode the daemon can read:
#   OpenSSH opens AuthorizedKeysFile as the target user, so root-only
#   0600 is denied, while 0640 still passes StrictModes).
# - Usernames, key material, and private keys are never logged.
#
# Env overrides (defaults suit the container; tests point them at tmpdirs):
#   SFTP_USERS_FILE     default /var/lib/sftp-users/users.json
#   SFTP_AUTH_KEYS_DIR  default /etc/ssh/authorized_keys.d
#   SFTP_GROUP          default sftpusers
#   SFTP_SHARE_ROOT     default /share (chroot root holding per-user dirs)
#   WEB_GID             default 2001 (group owning per-user directories)
#   WEB_GROUP           default webreaders (name for WEB_GID)
#   SFTP_SKIP_USERADD   default 0; when 1, only key files are managed
#                       (lets non-root CI exercise reconciliation).
set -eu

USERS_FILE="${SFTP_USERS_FILE:-/var/lib/sftp-users/users.json}"
AUTH_KEYS_DIR="${SFTP_AUTH_KEYS_DIR:-/etc/ssh/authorized_keys.d}"
SFTP_GROUP="${SFTP_GROUP:-sftpusers}"
SHARE_ROOT="${SFTP_SHARE_ROOT:-/share}"
WEB_GID="${WEB_GID:-2001}"
WEB_GROUP="${WEB_GROUP:-webreaders}"
SKIP_USERADD="${SFTP_SKIP_USERADD:-0}"

log() { printf 'reconcile: %s\n' "$*"; }

valid_username() {
  printf '%s' "$1" | grep -Eq '^[a-z_][a-z0-9_-]{0,31}$' 2>/dev/null
}

reserved_name() {
  case "$1" in
    root|admin|sshd|"${SFTP_GROUP}") return 0 ;;
    *) return 1 ;;
  esac
}

# Reject anything that is not a single-line OpenSSH public key.
valid_key() {
  printf '%s' "$1" | grep -Eq '^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp256|ecdsa-sha2-nistp384|ecdsa-sha2-nistp521|sk-ssh-ed25519@openssh\.com) [A-Za-z0-9+/=]+( [^[:cntrl:]]*)?$' 2>/dev/null
}

ensure_account() {
  # $1 = username. Creates a login-less account on first sight.
  # BusyBox adduser locks new accounts ('!'), and a locked account is
  # denied even with a valid key, so unlock after creation. An unlocked
  # account with no password hash still cannot use password auth
  # (disabled server-side), which keeps the key file the single source
  # of login truth: no key file means no login.
  if [ "$SKIP_USERADD" = "1" ]; then return 0; fi
  if ! id "$1" >/dev/null 2>&1; then
    adduser -S -H -s /sbin/nologin -G "$SFTP_GROUP" "$1" >/dev/null 2>&1 \
      || return 1
  fi
  hash="$(awk -F: -v user="$1" '$1 == user { print $2; exit }' /etc/shadow 2>/dev/null)"
  case "$hash" in
    '!'*)
      if command -v passwd >/dev/null 2>&1 && passwd -u "$1" >/dev/null 2>&1; then
        :
      elif command -v usermod >/dev/null 2>&1 && usermod -U "$1" >/dev/null 2>&1; then
        :
      else
        return 1
      fi
      ;;
  esac
}

ensure_web_group() {
  # Creates the web readers group (WEB_GROUP with GID WEB_GID) when missing,
  # mirroring the entrypoint's ensure_group for SFTP_GID. Best-effort: the
  # per-user directory chown uses the numeric WEB_GID, so a missing group
  # entry never blocks reconciliation. Always succeeds.
  if [ "$SKIP_USERADD" = "1" ]; then return 0; fi
  if [ "$(id -u)" != "0" ]; then return 0; fi
  if getent group "$WEB_GROUP" >/dev/null 2>&1; then return 0; fi
  if getent group "$WEB_GID" >/dev/null 2>&1; then return 0; fi
  addgroup -g "$WEB_GID" "$WEB_GROUP" >/dev/null 2>&1 || true
  return 0
}

ensure_user_dir() {
  # $1 = username. Creates /share/<username>/ owned by the user with group
  # WEB_GID and mode 0750: only the owner can write, the web service reads
  # via WEB_GID, and other SFTP users (members of SFTP_GROUP only) get
  # permission denied. Idempotent: an existing directory is left untouched
  # so re-runs never alter ownership or permissions.
  dir="$SHARE_ROOT/$1"
  if [ -d "$dir" ]; then return 0; fi
  if [ -e "$dir" ]; then return 1; fi
  if [ "$SKIP_USERADD" = "1" ]; then
    # Hermetic tests point SFTP_SHARE_ROOT at a tmpdir; ignore failures
    # when the real chroot is not writable.
    mkdir -p "$dir" 2>/dev/null || return 0
    chmod 0750 "$dir" 2>/dev/null || true
    return 0
  fi
  mkdir -p "$dir" 2>/dev/null || return 1
  chown "$1:$WEB_GID" "$dir" 2>/dev/null \
    || chown "$1" "$dir" 2>/dev/null || return 1
  chmod 0750 "$dir" || return 1
}

write_keys() {
  # $1 = username, $2 = newline-joined keys. Atomic root-owned 0640 write.
  dir="$AUTH_KEYS_DIR"
  mkdir -p "$dir"
  if [ "$(id -u)" = "0" ]; then
    chown root:root "$dir"
    chmod 0755 "$dir"
  fi
  tmp="$dir/.$1.tmp.$$"
  printf '%s\n' "$2" >"$tmp"
  chmod 0640 "$tmp"
  if [ "$(id -u)" = "0" ]; then
    chown root:"$SFTP_GROUP" "$tmp" 2>/dev/null || return 1
  fi
  mv -f "$tmp" "$dir/$1"
}

drop_keys() {
  # $1 = username. No effective key afterwards.
  rm -f "$AUTH_KEYS_DIR/$1"
}

# --- manifest present? ---
if [ ! -f "$USERS_FILE" ]; then
  log "no manifest at $USERS_FILE; nothing to do"
  exit 0
fi

# --- manifest valid? Any failure here changes nothing. ---
if ! jq -e 'type == "object" and .version == 1 and (.users | type == "array")' \
    "$USERS_FILE" >/dev/null 2>&1; then
  log "invalid manifest (need version=1 object with users array); keeping previous state"
  exit 1
fi

failures=0
keep_list=""

# The web readers group must exist before per-user directories are created
# with it (best-effort; numeric-GID chown works without the entry).
ensure_web_group

count="$(jq -r '.users | length' "$USERS_FILE" 2>/dev/null)" || {
  log "cannot read users array; keeping previous state"
  exit 1
}

i=0
while [ "$i" -lt "$count" ]; do
  user="$(jq -r --argjson i "$i" '.users[$i].username // empty' "$USERS_FILE")"
  enabled="$(jq -r --argjson i "$i" '.users[$i].enabled == true' "$USERS_FILE")"

  if ! valid_username "$user" || reserved_name "$user"; then
    log "skipping invalid username at index $i"
    failures=$((failures + 1))
    i=$((i + 1))
    continue
  fi

  if [ "$enabled" != "true" ]; then
    drop_keys "$user"
    log "disabled user=$user"
    i=$((i + 1))
    continue
  fi

  # A missing authorized_keys field or a non-array value is malformed
  # input: preserve the previous effective key, never broaden or revoke.
  if ! jq -e --argjson i "$i" '.users[$i] | has("authorized_keys") and (.authorized_keys | type == "array")' \
      "$USERS_FILE" >/dev/null 2>&1; then
    log "enabled user=$user has malformed authorized_keys; keeping previous state for user"
    keep_list="$keep_list $user"
    failures=$((failures + 1))
    i=$((i + 1))
    continue
  fi

  # Non-string entries are malformed input: preserve, do not coerce.
  if [ "$(jq -r --argjson i "$i" '[.users[$i].authorized_keys[] | select(type != "string")] | length' \
      "$USERS_FILE" 2>/dev/null)" != "0" ]; then
    log "enabled user=$user has invalid key material; keeping previous state for user"
    keep_list="$keep_list $user"
    failures=$((failures + 1))
    i=$((i + 1))
    continue
  fi

  # A valid empty authorization set is intentional revocation: remove any
  # stale effective key and succeed, so deleted final keys lose access.
  if [ "$(jq -r --argjson i "$i" '.users[$i].authorized_keys | length' \
      "$USERS_FILE" 2>/dev/null)" = "0" ]; then
    drop_keys "$user"
    log "enabled user=$user has empty authorization; revoked effective key"
    i=$((i + 1))
    continue
  fi

  # From here the user keeps an effective key unless provisioning fails: a
  # bad key or a failed write must not revoke a previously valid user.
  keep_list="$keep_list $user"

  keys="$(jq -r --argjson i "$i" '.users[$i].authorized_keys[]' \
    "$USERS_FILE" 2>/dev/null)"
  bad_key=0
  while IFS= read -r k; do
    if ! valid_key "$k"; then bad_key=1; break; fi
  done <<EOF
$keys
EOF
  if [ "$bad_key" -ne 0 ]; then
    log "enabled user=$user has invalid key material; keeping previous state for user"
    failures=$((failures + 1))
    i=$((i + 1))
    continue
  fi

  if ! ensure_account "$user"; then
    log "cannot provision account for user=$user; keeping previous state for user"
    failures=$((failures + 1))
    i=$((i + 1))
    continue
  fi
  # Per-user directory: the owner's private namespace. A failure here keeps
  # the previous key state (but never deletes an existing directory).
  if ! ensure_user_dir "$user"; then
    if [ "$SKIP_USERADD" = "1" ]; then
      log "skipping per-user directory for user=$user (user management skipped)"
    else
      log "cannot provision per-user directory for user=$user; keeping previous state for user"
      failures=$((failures + 1))
      i=$((i + 1))
      continue
    fi
  fi
  nkeys="$(printf '%s\n' "$keys" | wc -l)"
  write_keys "$user" "$keys"
  log "reconciled user=$user keys=$nkeys"
  i=$((i + 1))
done

# --- stale cleanup: key files for users no longer enabled lose effect. ---
if [ -d "$AUTH_KEYS_DIR" ]; then
  rm -f "$AUTH_KEYS_DIR"/.tmp.* 2>/dev/null || true
  for f in "$AUTH_KEYS_DIR"/*; do
    [ -e "$f" ] || continue
    base="${f##*/}"
    if ! valid_username "$base"; then continue; fi
    case " $keep_list " in
      *" $base "*) ;;
      *)
        drop_keys "$base"
        log "removed stale user=$base"
        ;;
    esac
  done
fi

if [ "$failures" -gt 0 ]; then
  log "$failures user(s) failed; others reconciled"
  exit 1
fi
exit 0
