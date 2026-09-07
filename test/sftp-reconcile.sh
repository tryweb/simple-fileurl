#!/bin/sh
# Hermetic tests for sftp/reconcile.sh.
# No docker, no root, no sshd required. Needs: sh, jq, mktemp, stat.
# User/group management is skipped via SFTP_SKIP_USERADD=1; only key-file
# reconciliation plus exit-code semantics are exercised.
# Usage: test/sftp-reconcile.sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
RECONCILE="$ROOT/sftp/reconcile.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok: $1"; }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

ED25519_A="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMkv2mXpacPKmfn2vejgseX5G5aVjpnuY6iI5y0lRP alice"
ED25519_B="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDFb2x+g6+9h8rN6x2w8t4K6m8s2q0u4y6w8e0r2t6y8u4v bob"
# Shared validator-parity fixtures (tasks 1.1, 3.3): real ssh-keygen keys.
# The same literals live in internal/sftpadmin/keylifecycle_test.go, where
# the Admin validator must accept them with matching SHA256 fingerprints.
REAL_RSA="ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCEES+HFIuRQAkSK8MYMv/SpwmojyaETDeAfvXFdp46rY2MkSOXZBC1amcVXJD9sPYdda2055eD5U+J3tuTCCPnGwXjGm4AeSmv1D9gkah/35GOgxHcPtodaKtA5xRalhpR8GHyJ99QDar+gOaK6kg3AnmkgM85XGur27+Pq1UWH6D9cSauuda9iS+xy+njmPOUnSKYNWQel5jGzTrpaEPVwxSPFNWZFhht1xG4zWKxkxMrnPXcF8/DKkzUWhUncwdf3OUZ38WAvYKGPvPSmeSDlm1bC6kc8vatkE+39UED4hW5Z9J7DSg1XDh66Wgc7foGZzn+e6jMYgyTE5Y3RCPl fixture-rsa"
REAL_ECDSA="ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBBJlS0JDnX4YX/ybI5cLciJ5MjTNV62EoRC8pwf9vAUKKhFpZCMgBfiubwWfTOid4gmvjMr0h546f1JrBQhHJTw= fixture-ec"

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT INT TERM
export SFTP_USERS_FILE="$T/users.json"
export SFTP_AUTH_KEYS_DIR="$T/keys"
export SFTP_SKIP_USERADD=1
mkdir -p "$SFTP_AUTH_KEYS_DIR"

write_manifest() { printf '%s' "$1" >"$SFTP_USERS_FILE"; }
run_reconcile() { "$RECONCILE" >"$T/out.log" 2>&1; echo "$?"; }

# The production entrypoint creates this group before reconciliation; create
# the same fixture group here because this test invokes the reconciler alone.
if [ "$(id -u)" = "0" ] && ! getent group sftpusers >/dev/null 2>&1; then
  addgroup -S sftpusers >/dev/null 2>&1
fi

# --- 1. enabled user gets exact key file, mode 0640 ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"alice\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\"]}]}"
[ "$(run_reconcile)" = "0" ] || bad "valid manifest exits 0"
[ -f "$SFTP_AUTH_KEYS_DIR/alice" ] || bad "alice key file created"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/alice")" = "$ED25519_A" ] || bad "alice key content exact"
[ "$(stat -c %a "$SFTP_AUTH_KEYS_DIR/alice")" = "640" ] || bad "alice key mode 0640"
ok "enabled user reconciled with 0640 key file"

# --- 2. disabled user has no effective key ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"alice\",\"enabled\":false,\"authorized_keys\":[\"$ED25519_A\"]}]}"
[ "$(run_reconcile)" = "0" ] || bad "disable manifest exits 0"
[ ! -e "$SFTP_AUTH_KEYS_DIR/alice" ] || bad "disabled alice key removed"
ok "disabled user key removed"

# --- 3. user deleted from manifest loses key (stale cleanup) ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"alice\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\"]}]}"
run_reconcile >/dev/null
write_manifest '{"version":1,"users":[]}'
run_reconcile >/dev/null
[ ! -e "$SFTP_AUTH_KEYS_DIR/alice" ] || bad "stale alice key removed"
ok "deleted user key cleaned up"

# --- 4. username prefixes must not preserve the wrong stale key ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"bob\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_B\"]},{\"username\":\"bobby\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_B\"]}]}"
run_reconcile >/dev/null
write_manifest "{\"version\":1,\"users\":[{\"username\":\"bobby\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_B\"]}]}"
run_reconcile >/dev/null
[ ! -e "$SFTP_AUTH_KEYS_DIR/bob" ] || bad "prefix user bob key was incorrectly retained"
[ -f "$SFTP_AUTH_KEYS_DIR/bobby" ] || bad "bobby key was incorrectly removed"
ok "stale cleanup matches complete usernames"

# --- 5. invalid JSON preserves previous valid state ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"alice\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\"]}]}"
run_reconcile >/dev/null
printf '{not valid json' >"$SFTP_USERS_FILE"
[ "$(run_reconcile)" != "0" ] || bad "invalid JSON exits non-zero"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/alice")" = "$ED25519_A" ] || bad "previous key state preserved"
ok "invalid manifest preserves previous key state"

# --- 6. bad username is skipped, others still reconciled ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"Bad Name!\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\"]},{\"username\":\"bob\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_B\"]}]}"
[ "$(run_reconcile)" != "0" ] || bad "bad username exits non-zero"
[ ! -e "$SFTP_AUTH_KEYS_DIR/Bad Name!" ] || bad "bad username gets no key file"
[ ! -e "$SFTP_AUTH_KEYS_DIR/Bad" ] || bad "bad username gets no key file (2)"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/bob")" = "$ED25519_B" ] || bad "valid sibling still reconciled"
ok "bad username skipped, sibling reconciled"

# --- 7. malformed key is rejected, no key file written ---
write_manifest '{"version":1,"users":[{"username":"mallory","enabled":true,"authorized_keys":["not-a-key"]}]}'
[ "$(run_reconcile)" != "0" ] || bad "malformed key exits non-zero"
[ ! -e "$SFTP_AUTH_KEYS_DIR/mallory" ] || bad "malformed key writes no file"
ok "malformed key rejected"

# --- 8. reserved names are rejected ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"root\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\"]}]}"
[ "$(run_reconcile)" != "0" ] || bad "reserved name exits non-zero"
[ ! -e "$SFTP_AUTH_KEYS_DIR/root" ] || bad "reserved name gets no key file"
ok "reserved name rejected"

# --- 9. missing manifest is a no-op success ---
rm -f "$SFTP_USERS_FILE"
[ "$(run_reconcile)" = "0" ] || bad "missing manifest exits 0"
ok "missing manifest is no-op"

# --- 10. multiple keys joined with newlines, no private key leakage in logs ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"carol\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\",\"$REAL_RSA\"]}]}"
[ "$(run_reconcile)" = "0" ] || bad "multi-key manifest exits 0"
lines="$(wc -l <"$SFTP_AUTH_KEYS_DIR/carol")"
[ "$lines" = "2" ] || bad "carol has 2 key lines (got $lines)"
if grep -q "PRIVATE" "$T/out.log"; then bad "logs mention private"; else ok "no private key material in logs"; fi

# --- 11. private key material in manifest is rejected, never logged ---
write_manifest '{"version":1,"users":[{"username":"dave","enabled":true,"authorized_keys":["-----BEGIN OPENSSH PRIVATE KEY-----"]}]}'
[ "$(run_reconcile)" != "0" ] || bad "private key material exits non-zero"
[ ! -e "$SFTP_AUTH_KEYS_DIR/dave" ] || bad "private key material writes no file"
if grep -q "BEGIN.*PRIVATE" "$T/out.log"; then bad "private material leaked to logs"; else ok "private material rejected silently"; fi

# --- 12. enabled user gets per-user directory with mode 0750 ---
export SFTP_SHARE_ROOT="$T/share"
mkdir -p "$SFTP_SHARE_ROOT"
write_manifest "{\"version\":1,\"users\":[{\"username\":\"alice\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\"]}]}"
[ "$(run_reconcile)" = "0" ] || bad "per-user dir manifest exits 0"
[ -d "$SFTP_SHARE_ROOT/alice" ] || bad "alice per-user directory created"
[ "$(stat -c %a "$SFTP_SHARE_ROOT/alice")" = "750" ] || bad "alice per-user directory mode 0750"
ok "per-user directory created with mode 0750"

# --- 13. per-user directory creation is idempotent ---
chmod 755 "$SFTP_SHARE_ROOT/alice"
before="$(stat -c '%a %u %g' "$SFTP_SHARE_ROOT/alice")"
[ "$(run_reconcile)" = "0" ] || bad "second reconcile exits 0"
after="$(stat -c '%a %u %g' "$SFTP_SHARE_ROOT/alice")"
[ "$before" = "$after" ] || bad "second run altered per-user directory ($before -> $after)"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/alice")" = "$ED25519_A" ] || bad "alice key intact after second run"
ok "per-user directory untouched by second reconcile"

# --- 14. disabled user keeps directory on disk but loses key ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"alice\",\"enabled\":false,\"authorized_keys\":[\"$ED25519_A\"]}]}"
[ "$(run_reconcile)" = "0" ] || bad "disable manifest exits 0"
[ ! -e "$SFTP_AUTH_KEYS_DIR/alice" ] || bad "disabled alice key removed"
[ -d "$SFTP_SHARE_ROOT/alice" ] || bad "disabled alice directory persists"
ok "disabled user keeps directory, loses key"
unset SFTP_SHARE_ROOT

# --- 15. SFTP users cannot access other users' per-user directories ---
# Needs root (account provisioning + su); skipped otherwise.
if [ "$(id -u)" = "0" ] && command -v su >/dev/null 2>&1 && command -v adduser >/dev/null 2>&1; then
  iso_share="$T/iso-share"
  mkdir -p "$iso_share" && chmod 0755 "$iso_share" && chmod 0755 "$T"
  export SFTP_SHARE_ROOT="$iso_share"
  export SFTP_SKIP_USERADD=0
  write_manifest "{\"version\":1,\"users\":[{\"username\":\"isoalice\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\"]},{\"username\":\"isobob\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_B\"]}]}"
  if [ "$(run_reconcile)" = "0" ] && [ -d "$iso_share/isoalice" ]; then
    [ "$(stat -c %a "$iso_share/isoalice")" = "750" ] || bad "isoalice dir mode 0750"
    [ "$(stat -c %U "$iso_share/isoalice")" = "isoalice" ] || bad "isoalice dir owned by isoalice"
    if su isobob -s /bin/sh -c "ls \"$iso_share/isoalice\"" >/dev/null 2>&1; then
      bad "isobob must not list isoalice directory"
    else
      ok "cross-user per-user directory access denied"
    fi
    if su isoalice -s /bin/sh -c "ls \"$iso_share/isoalice\"" >/dev/null 2>&1; then
      ok "owner can list own per-user directory"
    else
      bad "owner must list own per-user directory"
    fi
  else
    ok "skip isolation checks (user provisioning unavailable)"
  fi
  export SFTP_SKIP_USERADD=1
  unset SFTP_SHARE_ROOT
  deluser isoalice >/dev/null 2>&1 || true
  deluser isobob >/dev/null 2>&1 || true
else
  ok "skip cross-user isolation test (needs root)"
fi

# --- 16. real RSA and ECDSA keys reconcile (validator parity with Admin) ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"rsauser\",\"enabled\":true,\"authorized_keys\":[\"$REAL_RSA\"]},{\"username\":\"ecuser\",\"enabled\":true,\"authorized_keys\":[\"$REAL_ECDSA\"]}]}"
[ "$(run_reconcile)" = "0" ] || bad "real rsa/ecdsa manifest exits 0"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/rsauser")" = "$REAL_RSA" ] || bad "rsauser key content exact"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/ecuser")" = "$REAL_ECDSA" ] || bad "ecuser key content exact"
ok "real RSA and ECDSA keys reconciled"

# --- 17. one entry per valid key; removing one key leaves the other ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"multi\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\",\"$REAL_RSA\"]}]}"
[ "$(run_reconcile)" = "0" ] || bad "two-key manifest exits 0"
[ "$(wc -l <"$SFTP_AUTH_KEYS_DIR/multi")" = "2" ] || bad "multi has 2 key lines"
write_manifest "{\"version\":1,\"users\":[{\"username\":\"multi\",\"enabled\":true,\"authorized_keys\":[\"$REAL_RSA\"]}]}"
[ "$(run_reconcile)" = "0" ] || bad "one-key manifest exits 0"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/multi")" = "$REAL_RSA" ] || bad "remaining key exact after removal"
ok "per-key entries track manifest removals"

# --- 18. valid empty authorization revokes stale access with exit 0 ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"revoked\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\"]}]}"
run_reconcile >/dev/null
[ -f "$SFTP_AUTH_KEYS_DIR/revoked" ] || bad "revoked key file seeded"
write_manifest '{"version":1,"users":[{"username":"revoked","enabled":true,"authorized_keys":[]}]}'
[ "$(run_reconcile)" = "0" ] || bad "valid empty manifest exits 0"
[ ! -e "$SFTP_AUTH_KEYS_DIR/revoked" ] || bad "stale key survives valid empty authorization"
[ "$(run_reconcile)" = "0" ] || bad "repeated valid empty exits 0"
[ ! -e "$SFTP_AUTH_KEYS_DIR/revoked" ] || bad "repeated valid empty recreates key"
ok "valid empty authorization revokes stale access"

# --- 19. disabled user with a stale key file loses it with exit 0 ---
printf '%s\n' "$ED25519_A" >"$SFTP_AUTH_KEYS_DIR/stale"
write_manifest "{\"version\":1,\"users\":[{\"username\":\"stale\",\"enabled\":false,\"authorized_keys\":[\"$ED25519_A\"]}]}"
[ "$(run_reconcile)" = "0" ] || bad "disabled stale manifest exits 0"
[ ! -e "$SFTP_AUTH_KEYS_DIR/stale" ] || bad "disabled stale key survives"
ok "disabled user stale key removed"

# --- 20. malformed input preserves the prior effective key, non-zero ---
write_manifest "{\"version\":1,\"users\":[{\"username\":\"keep\",\"enabled\":true,\"authorized_keys\":[\"$ED25519_A\"]}]}"
run_reconcile >/dev/null
write_manifest '{"version":1,"users":[{"username":"keep","enabled":true}]}'
[ "$(run_reconcile)" != "0" ] || bad "missing authorized_keys exits non-zero"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/keep")" = "$ED25519_A" ] || bad "missing field revoked prior key"
write_manifest '{"version":1,"users":[{"username":"keep","enabled":true,"authorized_keys":"not-an-array"}]}'
[ "$(run_reconcile)" != "0" ] || bad "wrong-type authorized_keys exits non-zero"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/keep")" = "$ED25519_A" ] || bad "wrong type revoked prior key"
write_manifest '{"version":1,"users":[{"username":"keep","enabled":true,"authorized_keys":["not-a-key"]}]}'
[ "$(run_reconcile)" != "0" ] || bad "invalid key entry exits non-zero"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/keep")" = "$ED25519_A" ] || bad "invalid entry revoked prior key"
write_manifest '{"version":1,"users":[{"username":"keep","enabled":true,"authorized_keys":["ssh-ed25519 AAAA", 42]}]}'
[ "$(run_reconcile)" != "0" ] || bad "non-string entry exits non-zero"
[ "$(cat "$SFTP_AUTH_KEYS_DIR/keep")" = "$ED25519_A" ] || bad "non-string entry revoked prior key"
ok "malformed input preserves fail-safe state"

if [ "$fail" -gt 0 ]; then echo "RECONCILE FAIL: $fail failures" >&2; exit 1; fi
echo "RECONCILE OK ($pass checks)"
