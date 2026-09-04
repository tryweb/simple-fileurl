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
# - Disabled users and users absent from the manifest have no key file.
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
#   SFTP_SKIP_USERADD   default 0; when 1, only key files are managed
#                       (lets non-root CI exercise reconciliation).
set -eu

USERS_FILE="${SFTP_USERS_FILE:-/var/lib/sftp-users/users.json}"
AUTH_KEYS_DIR="${SFTP_AUTH_KEYS_DIR:-/etc/ssh/authorized_keys.d}"
SFTP_GROUP="${SFTP_GROUP:-sftpusers}"
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

  # Keep an existing effective key when this user's new entry is invalid or
  # provisioning fails; a bad manifest must not revoke a previously valid user.
  keep_list="$keep_list $user"

  keys="$(jq -r --argjson i "$i" \
    '.users[$i].authorized_keys // [] | map(select(type == "string")) | .[]' \
    "$USERS_FILE" 2>/dev/null)"
  if [ -z "$keys" ]; then
    log "enabled user=$user has no keys; keeping previous state for user"
    failures=$((failures + 1))
    i=$((i + 1))
    continue
  fi
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
