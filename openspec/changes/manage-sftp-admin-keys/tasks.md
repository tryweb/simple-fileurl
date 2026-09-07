## 0. Planning prerequisites

- [x] 0.1 Add the companion `specs/sftp-service/spec.md` delta by copying the complete runtime reconciliation requirement from the main `sftp-service` spec; verify strict OpenSpec validation sees both modified capabilities before implementation

## 1. Manifest and reconciliation foundations

- [x] 1.1 Add canonical fingerprint helpers and a shared Go/shell fixture matrix for Ed25519, RSA, ECDSA, comments, whitespace, malformed keys, and unsupported types; verify Admin-accepted keys are accepted by the reconciler and rejected malformed keys are not
- [x] 1.2 Add an atomic manifest-wide repair mutation under the store mutex while keeping ordinary `Store.Update` strict; verify mixed users, two users with invalid entries, all-invalid users, no-op repair, preservation of valid keys, and zero-valid-key disablement with focused store tests
- [x] 1.3 Define valid empty-key reconciliation as effective authorization removal while preserving malformed/invalid-input fail-safe behavior; seed stale effective files and verify empty-valid and disabled cases remove them with exit 0, while malformed cases preserve them with non-zero status
- [x] 1.4 Add regression coverage proving a user with multiple keys produces one authorized-key entry per valid key and that removing one key leaves the others effective

## 2. Admin API and security semantics

- [x] 2.1 Add authenticated, CSRF-protected `POST /users/delete-key` using username plus canonical `SHA256:` fingerprint; verify `401/403/400/404/409/500`, unknown and repeated deletes leave the manifest unchanged, and successful removal is atomic
- [x] 2.2 Add authenticated, CSRF-protected manifest-wide `POST /users/remove-invalid-keys`; verify exact statuses, two-user repair, mixed valid/invalid entries, all-invalid disablement, no-op success, and preservation of unrelated user fields
- [x] 2.3 Update disable/enable handling so disabling retains manifest keys but removes effective access and re-enabling restores them; verify new logins fail, zero-key enable returns `409` without enabling, and no session-termination signal is sent
- [x] 2.4 Make final usable-key removal atomically disable the user and leave a valid empty key set; seed stale authorization and verify reconciliation removes it with no effective login

## 3. Generated key onboarding

- [x] 3.1 Add authenticated, CSRF-protected `POST /users/generate-key` for existing users, appending a generated Ed25519 public key without re-enabling disabled users; verify exact statuses, distinct fingerprints, and preservation of existing keys
- [x] 3.2 Reuse the pending-key one-time download flow with a ten-minute TTL, atomic consume, restart invalidation, and `Cache-Control: no-store`; verify private material never reaches the manifest, persistence, or logs, first download is `200`, and second/expired/unknown/post-restart downloads are `404`
- [x] 3.3 Retain `POST /users/add-key` for advanced external-key registration while keeping validation and idempotent append behavior; verify existing clients, CSRF enforcement, malformed-key rejection, and validator-parity fixtures remain compatible

## 4. Admin Users UI

- [x] 4.1 Replace the primary existing-user paste action with `Generate new key` and a one-time download result showing the new fingerprint; retain create-user paste as Advanced compatibility; verify CSRF protection and disabled-user behavior
- [x] 4.2 Render every valid key by canonical fingerprint and key type with an individual Remove action, keep Disable/Enable distinct, and expose no per-key action for invalid rows; verify multiple key types and status transitions in UI handler tests
- [x] 4.3 Replace bare `(invalid)` output with an explicit invalid state and manifest-wide `Remove invalid keys` action; verify repair is available when invalid entries block ordinary writes and its scope is clear in the UI

## 5. End-to-end verification and documentation

- [x] 5.1 Extend focused HTTP/store integration tests for generate, download, delete, repair, disable, re-enable, CSRF, zero-key enable, and last-key flows; verify exact response contracts and that the manifest never contains private-key material
- [x] 5.2 Run `gofmt`, `go vet ./...`, `go test -race -shuffle=on -count=1 ./...`, `bash test/install-upgrade.sh`, `bash test/sftp-config.sh`, and `bash test/sftp-reconcile.sh`; expect all checks to pass
- [ ] 5.3 Run the dev Compose stack and verify Admin login, generated-key download, remote Admin reachability, SFTP login/revocation, stale-file removal, and matched-image rollback checks through the documented endpoints
