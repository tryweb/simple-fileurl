# Manage SFTP admin keys

## Why

The Admin Users page stores a list of authorized keys, but operators cannot manage individual keys safely. Legacy or malformed entries appear only as `(invalid)` and can block unrelated manifest updates, while adding a new key requires manually pasting public-key text even though the service already supports one-time generated-key delivery.

## What Changes

- Add per-user key lifecycle management: list keys by fingerprint, generate additional keys, and remove an individual key.
- Define disable semantics: disabling a user removes effective SSH authorization while retaining manifest keys for later re-enable; existing sessions are not forcibly disconnected.
- Define safe last-key removal: removing the final usable key atomically disables the user and ensures no effective authorized-key file remains.
- Add manifest-wide invalid-key repair that removes every invalid entry while preserving valid keys; users with no valid keys become disabled. The operation is manifest-wide because ordinary `Store.Update` validates the complete manifest.
- Replace the primary existing-user `Add key` paste action with generated Ed25519 keypairs and the existing single-use private-key download flow.
- Retain the external public-key registration endpoint for migration and advanced use, but make it secondary to generated-key onboarding.
- Align SFTP reconciliation with the explicit empty-key state so a valid empty key set cannot retain stale access; a companion `sftp-service` delta is required before implementation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `sftp-admin`: Extend the Admin Users contract with multi-key lifecycle operations, invalid-key repair, generated key onboarding, and explicit disable/last-key security semantics.
- `sftp-service`: Modify runtime reconciliation so intentional empty authorization revokes access while malformed input remains fail-safe.

## Impact

- `internal/sftpadmin`: new key mutation handlers, generated-key flow reuse, invalid-key cleanup, and UI actions.
- `sftp/reconcile.sh`: distinguish a valid empty key set from a malformed manifest and remove effective authorization when access is revoked.
- Existing `users.json` remains schema-compatible; legacy invalid entries become repairable through the Admin UI.
- Enabling a user with no usable keys is rejected with `409 Conflict`; adding or generating a key never implicitly enables a disabled user.
- All state-changing form endpoints use the existing authentication and CSRF funnel with explicit status and redirect behavior.
- Tests must cover API authorization/CSRF, atomic manifest updates, one-time private-key delivery, multi-key reconciliation, invalid-key repair, disable behavior, and last-key removal.
- Admin and SFTP changes must be deployed as a matched release; rolling back only the reconciler after empty-key revocation is unsafe.
