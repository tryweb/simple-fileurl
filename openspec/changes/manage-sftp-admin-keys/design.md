## Context

The existing `sftp-admin` manifest already stores `User.AuthorizedKeys` as a list and the current add-key operation appends validated keys. Generated keys already use an in-memory pending entry with a ten-minute, single-use download. The remaining design problem is coordinating individual key lifecycle operations with tolerant reads of legacy invalid keys and the SFTP reconciler's fail-safe behavior.

`Store.Load` tolerates legacy invalid entries, but ordinary `Store.Update` validates the complete final manifest strictly. Consequently, unrelated ordinary writes remain blocked while any invalid entry exists anywhere in the manifest; only the dedicated repair path may remove those entries.

See `proposal.md` and `specs/sftp-admin/spec.md` for the motivation and observable contract. A companion `specs/sftp-service/spec.md` delta must be created before implementation because reconciliation semantics change.

## Goals / Non-Goals

**Goals:**

- Add safe per-key removal, manifest-wide invalid-key repair, and generated-key onboarding without changing the `users.json` schema.
- Make disabled users unable to start new SFTP sessions while retaining their manifest keys for explicit re-enable.
- Make final-key removal and empty-key repair revoke effective access deterministically.
- Preserve the existing one-time private-key delivery guarantees.

**Non-Goals:**

- Replacing public-key authentication or adding user passwords.
- Terminating already-established SFTP sessions when a user is disabled.
- Removing the advanced external public-key registration API.
- Automatically deleting or rewriting legacy invalid keys during unrelated mutations.

## Decisions

### 1. Keep the manifest schema and identify keys by fingerprint

Keep `AuthorizedKeys []string` and store canonical public-key text as today. The UI and delete operation identify valid keys by the SHA256 digest of the decoded SSH public-key wire blob, rendered as `SHA256:` plus unpadded standard Base64, matching `ssh-keygen -l -E sha256`. Comments and surrounding whitespace do not affect identity. The action is scoped to the username and never uses array position or raw key text.

The invalid-key repair operation is manifest-wide and removes every entry that fails canonical validation. This scope is required because an invalid entry has no reliable fingerprint and strict final-manifest validation would reject a user-scoped update whenever another user still contains an invalid entry. Repair runs under the store mutex, preserves all valid keys and unrelated user fields, and disables users left with zero valid keys. Ordinary `Store.Update` remains strict and never silently repairs.

### 2. Use authenticated form POST endpoints consistent with the current server

Add these operations:

- `POST /users/delete-key` with `username`, `fingerprint`, and CSRF token.
- `POST /users/remove-invalid-keys` with a CSRF token; it repairs the complete manifest and does not accept a username.
- `POST /users/generate-key` with `username` and CSRF token.

Keep `POST /users/add-key` for advanced external-key registration. Every state-changing form uses the existing authentication and CSRF funnel. The response contract is explicit: unauthenticated `401`; invalid CSRF `403` without mutation; malformed input `400`; unknown user/key `404`; strict-manifest conflict or zero-key enable `409`; persistence or generation failure `500`; ordinary successful mutation `303` to `/`; successful generation `200` with its download page. A repeated delete for an absent fingerprint is `404` and does not mutate.

### 3. Define disable and final-key behavior at both manifest and effective-access layers

Disabling a user sets `Enabled=false` but retains valid keys in the manifest. The reconciler removes the user's effective authorized-key file, so new SFTP authentication fails; it does not attempt to terminate active sessions. Re-enabling restores the retained valid keys.

Deleting the final usable key, or repairing a user whose keys are all invalid, sets `Enabled=false` in the same atomic manifest mutation and leaves a valid empty key set. An enabled user with a valid empty key list is an intentional revoked state: the reconciler removes the effective file and exits successfully. Malformed JSON, missing fields, wrong field types, or invalid key material preserve the previous effective state and return non-zero. Enabling a user with zero usable keys is rejected with `409`, leaving the user disabled.

This closes the current ambiguity where an enabled user with an empty key list can retain a stale authorized-key file. A companion `sftp-service` delta must copy the complete runtime reconciliation requirement and document this empty-versus-malformed distinction.

### 4. Reuse the existing generated-key delivery mechanism

`/users/generate-key` calls the same Ed25519 generator and validation path used by user creation, appends only the public key, and places the private half in the existing pending-key map. The generated page shows the new fingerprint and one-time download link. Pending private material is deleted before serving, consumed under the pending-key lock, expires ten minutes after generation, and is invalid after process restart. Download responses use `Cache-Control: no-store`.

The generated key remains in the manifest if the administrator loses the download, so the administrator can remove it by fingerprint and generate a replacement. Generation for a disabled user does not enable the user; it prepares a key for a later explicit re-enable. The UI disables the submit control after activation to reduce accidental duplicate generation.

### 5. Make generated keys primary in the UI while retaining migration support

Replace the primary existing-user public-key textarea action with `Generate new key`. Keep external registration available as an advanced operation for migration and administrators who already manage keys elsewhere; the existing create-user paste-or-generate flow remains compatible. Each row renders canonical fingerprints and key types, with per-key Remove actions and a user-level Disable/Enable action. Invalid rows expose only the manifest-wide `Remove invalid keys` action and never show a guessed fingerprint.

### 6. Require validator parity without adding a dependency

Admin and shell validation SHALL use the same supported-key allowlist and canonical single-line OpenSSH fixtures. The reconciler must fail closed and must not accept arbitrary Base64 that Admin validation rejects. Add parity fixtures for Ed25519, RSA, ECDSA, comments, whitespace, malformed keys, and unsupported types. A future shared validator implementation is separate from this change.

## Risks / Trade-offs

- **Generated private key is lost before download** → Keep the public key removable by fingerprint, retain the existing ten-minute TTL and delete-before-serve single-use behavior, and show a prominent download warning.
- **Disable does not terminate active SFTP sessions** → State this explicitly in the UI/spec; effective authorization is removed for all new connections.
- **Legacy invalid entries can block ordinary writes** → Allow only the dedicated manifest-wide repair mutation to filter invalid entries and validate the final manifest; preserve valid entries and other user fields atomically.
- **Validator behavior differs between Admin and shell reconciler** → Use cross-validator fixtures and fail closed; do not weaken Admin validation or add a new dependency.
- **Repeated generation creates multiple active keys** → Make each generated fingerprint visible, disable the submit control after activation, and retain per-key removal so accidental duplicates are recoverable.
- **Mixed-version rollback can retain stale access** → Deploy matching immutable Admin/SFTP releases and do not roll back only the reconciler after an empty-key manifest has been written.

## Migration Plan

1. Add and validate the companion `sftp-service` delta before implementation so key lifecycle writes and empty-key reconciliation have matching semantics.
2. Deploy the Admin and SFTP changes together as a matched immutable release.
3. Do not rewrite existing manifests automatically; existing valid keys and disabled states remain unchanged.
4. Administrators use the Users page's manifest-wide invalid-key repair action for legacy entries, then generate replacement keys as needed.
5. Existing external public-key users continue using `POST /users/add-key`; the UI's generated-key flow is an additive onboarding path.
6. Roll back only to a matched previous Admin/SFTP release. Do not roll back only the reconciler after a valid empty-key manifest has been written; avoid new lifecycle endpoints until the newer Admin image is restored.
