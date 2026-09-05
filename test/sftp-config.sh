#!/bin/sh
# Contract tests for sftp/sshd_config and Dockerfile.sftp.
# Static checks only: no docker, no sshd required.
# Usage: test/sftp-config.sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
CFG="$ROOT/sftp/sshd_config"
DOCKERFILE="$ROOT/Dockerfile.sftp"
ENTRYPOINT="$ROOT/sftp/entrypoint.sh"
RECONCILE="$ROOT/sftp/reconcile.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok: $1"; }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

need_file() {
  if [ -f "$1" ]; then ok "exists $1"; else bad "missing $1"; fi
}

# opt <file> <directive> <expected-value>
# Matches "Directive value" case-insensitively, ignoring leading space.
opt() {
  if grep -Eiq "^[[:space:]]*$2[[:space:]]+$3([[:space:]]|$)" "$1"; then
    ok "$1: $2 $3"
  else
    bad "$1: want '$2 $3'"
  fi
}

need_file "$CFG"
need_file "$DOCKERFILE"
need_file "$ENTRYPOINT"
need_file "$RECONCILE"
[ "$fail" -gt 0 ] && { echo "CONFIG FAIL: $fail failures" >&2; exit 1; }

# --- sshd_config hardening contract ---
opt "$CFG" PasswordAuthentication no
opt "$CFG" KbdInteractiveAuthentication no
opt "$CFG" PubkeyAuthentication yes
opt "$CFG" AuthorizedKeysFile /etc/ssh/authorized_keys.d/%u
opt "$CFG" ChrootDirectory /share
opt "$CFG" ForceCommand "internal-sftp -u 0002"
opt "$CFG" PermitTTY no
opt "$CFG" AllowTcpForwarding no
opt "$CFG" X11Forwarding no
opt "$CFG" PermitTunnel no
opt "$CFG" AllowAgentForwarding no
opt "$CFG" AllowGroups sftpusers
opt "$CFG" Subsystem "sftp internal-sftp -u 0002"
opt "$CFG" PermitRootLogin no
opt "$CFG" PermitEmptyPasswords no

# AuthorizedKeysFile must be absolute and outside the chroot.
if grep -Eiqi '^[[:space:]]*AuthorizedKeysFile[[:space:]]+/etc/ssh/authorized_keys\.d/%u' "$CFG"; then
  ok "AuthorizedKeysFile is absolute and outside chroot"
else
  bad "AuthorizedKeysFile must be /etc/ssh/authorized_keys.d/%u"
fi

# StrictModes must never be disabled.
if grep -Eiq '^[[:space:]]*StrictModes[[:space:]]+no' "$CFG"; then
  bad "StrictModes no is forbidden"
else
  ok "StrictModes is not disabled"
fi

# No password auth backdoors.
if grep -Eiq '^[[:space:]]*(PasswordAuthentication|KbdInteractiveAuthentication|ChallengeResponseAuthentication)[[:space:]]+yes' "$CFG"; then
  bad "password/keyboard-interactive auth must not be enabled"
else
  ok "no password auth backdoor"
fi

# --- Dockerfile.sftp conventions (Alpine, like Dockerfile) ---
if grep -Eq '^FROM alpine:3\.23' "$DOCKERFILE"; then ok "Dockerfile.sftp pins alpine:3.23"; else bad "Dockerfile.sftp must pin alpine:3.23"; fi
if grep -Eq 'apk add --no-cache' "$DOCKERFILE"; then ok "uses apk --no-cache"; else bad "want 'apk add --no-cache'"; fi
if grep -Eq 'openssh' "$DOCKERFILE"; then ok "installs openssh"; else bad "want openssh installed"; fi
if grep -Eq 'COPY sftp/sshd_config /etc/ssh/sshd_config' "$DOCKERFILE"; then ok "copies sshd_config"; else bad "want COPY sftp/sshd_config"; fi
if grep -Eq 'EXPOSE 22' "$DOCKERFILE"; then ok "EXPOSE 22"; else bad "want EXPOSE 22"; fi
if grep -Eq 'ENTRYPOINT.*entrypoint\.sh' "$DOCKERFILE"; then ok "entrypoint wired"; else bad "want ENTRYPOINT entrypoint.sh"; fi

# --- entrypoint/reconciler wiring contract ---
if grep -Eq 'ChrootDirectory|/share' "$ENTRYPOINT"; then ok "entrypoint references /share"; else bad "entrypoint must reference /share"; fi
if ! grep -Eq 'SFTP_SEED_USER|SFTP_SEED_PUBKEY' "$ENTRYPOINT" && grep -Eq 'seedIfAbsent|SFTP_SEED_USER' "$ROOT/cmd/sftp-admin/main.go"; then
  ok "admin is the only manifest seeder"
else
  bad "manifest seeding must be owned by sftp-admin only"
fi
if grep -Eq 'users\.json|USERS_FILE' "$RECONCILE"; then ok "reconciler reads users manifest"; else bad "reconciler must read users manifest"; fi
if grep -Eq '0640' "$RECONCILE"; then ok "reconciler writes 0640 key files"; else bad "reconciler must write 0640 key files"; fi

# --- per-user directory contract (add-per-user-directories) ---
if grep -Eq 'WEB_GID' "$ENTRYPOINT"; then ok "entrypoint references WEB_GID"; else bad "entrypoint must reference WEB_GID"; fi
if grep -Eq 'ensure_web_group' "$ENTRYPOINT"; then ok "entrypoint ensures web readers group"; else bad "entrypoint must ensure web readers group"; fi
if grep -Eq 'WEB_GID' "$RECONCILE"; then ok "reconciler uses WEB_GID"; else bad "reconciler must reference WEB_GID"; fi
if grep -Eq '0750' "$RECONCILE"; then ok "reconciler creates 0750 per-user dirs"; else bad "reconciler must create 0750 per-user dirs"; fi
if grep -Eq 'ensure_user_dir|per-user' "$RECONCILE"; then ok "reconciler manages per-user dirs"; else bad "reconciler must manage per-user dirs"; fi

if [ "$fail" -gt 0 ]; then echo "CONFIG FAIL: $fail failures" >&2; exit 1; fi
echo "CONFIG OK ($pass checks)"
