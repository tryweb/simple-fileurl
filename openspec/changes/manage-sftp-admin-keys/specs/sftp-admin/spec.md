## MODIFIED Requirements

### Requirement: Register external public key

The Admin system SHALL support registering an externally generated public key (e.g. from the user's own `ssh-keygen`) for either a new or existing SFTP user through an authenticated advanced/API operation. It SHALL validate the key type and format, display its canonical fingerprint in the Users UI, and append it idempotently to the user's authorized-key set rather than silently replacing other keys. Invalid key material SHALL be rejected with a clear client error. The primary existing-user Add key UI action SHALL use generated keypairs; the external registration endpoint and any explicitly advanced paste flow remain available for migration.

Fingerprints SHALL be computed from the decoded SSH public-key wire blob as `SHA256:` followed by unpadded standard Base64, matching `ssh-keygen -l -E sha256`; comments and surrounding whitespace SHALL not affect identity.

#### Scenario: Register own public key

- **WHEN** an authenticated administrator submits a valid `ssh-ed25519` public key for user `bob` to `POST /users/add-key`
- **THEN** the key is appended to `bob`'s authorized-key set, its fingerprint is shown in the Users UI, and `bob` can SFTP-login with the matching private key

#### Scenario: Malformed key is rejected

- **WHEN** the administrator submits text that is not a valid SSH public key
- **THEN** the request fails with a clear client error and nothing is stored

#### Scenario: Multiple keys are preserved

- **WHEN** the administrator registers a second distinct key for an existing user
- **THEN** both keys remain effective and submitting the same key again does not create a duplicate

### Requirement: List and revoke SFTP users

The Admin UI SHALL list all SFTP users and SHALL allow disabling or re-enabling a user without deleting the user's manifest keys. Disabling SHALL be written atomically and SHALL remove the user's effective authorized-key file. After disabling, that user's new SFTP login attempts SHALL be rejected while other users are unaffected; existing SFTP sessions are not required to be disconnected. Re-enabling SHALL restore the retained valid keys without requiring key re-entry. Enabling a user with no usable keys SHALL fail with `409 Conflict` and leave the user disabled; adding or generating a key SHALL never implicitly enable a disabled user.

#### Scenario: Disabled user with retained keys cannot start a new session

- **WHEN** an administrator disables user `alice` while `alice` still has authorized keys
- **THEN** `alice` remains in the manifest with those keys retained, the effective authorized-key file is removed, and new SFTP authentication attempts for `alice` are rejected

#### Scenario: Re-enabled user restores retained keys

- **WHEN** an administrator re-enables a previously disabled user
- **THEN** the user's retained valid keys become effective again and a matching SFTP client can log in

#### Scenario: Existing sessions are not forcibly disconnected

- **WHEN** an administrator disables a user with an already-established SFTP session
- **THEN** the disable operation does not promise to terminate that existing session, but all subsequent login attempts are rejected

#### Scenario: Revoked user cannot log in

- **WHEN** an administrator disables user `alice`
- **THEN** `alice`'s subsequent SFTP authentication fails and other users can still log in

## ADDED Requirements

### Requirement: Manage multiple authorized keys

The Admin Users UI SHALL display every authorized key for a user by canonical fingerprint and key type, and SHALL allow an administrator to remove one selected valid key without replacing or reordering the remaining keys. The operation SHALL require an authenticated session and CSRF protection, identify the key by fingerprint rather than list position, and persist the manifest atomically. Invalid entries SHALL not expose a per-key removal action because they have no reliable fingerprint.

#### Scenario: Remove one of several valid keys

- **WHEN** an administrator submits `POST /users/delete-key` with an existing username and one key's `SHA256:` fingerprint
- **THEN** only that key is removed, all other valid keys remain unchanged, and the removed key can no longer authenticate a new SFTP session after reconciliation

#### Scenario: Unknown key fingerprint is rejected

- **WHEN** an administrator requests removal with a fingerprint that is not assigned to the user
- **THEN** the request fails with a clear not-found error and the user's manifest is unchanged

#### Scenario: Removing the final usable key disables the user

- **WHEN** an administrator removes the only usable key from an enabled user
- **THEN** the user is atomically marked disabled, the manifest retains the user with no usable keys, and the effective authorized-key file is absent

### Requirement: Repair invalid authorized keys

The Admin Users UI SHALL identify stored public-key entries that fail canonical validation as invalid and SHALL provide an authenticated, CSRF-protected operation to remove all invalid keys in the manifest. Repair SHALL preserve every valid key and every unrelated user field, persist the complete result atomically, and SHALL be permitted to produce valid empty key sets. If no valid keys remain for a user after repair, that user SHALL be disabled so no stale authorization remains effective. Repair SHALL be a no-op success when no invalid entries exist.

#### Scenario: Repair removes invalid entries and preserves valid entries

- **WHEN** an administrator submits `POST /users/remove-invalid-keys` with CSRF protection and the manifest contains invalid and valid entries for one or more users
- **THEN** all invalid entries in the manifest are removed, every valid key and its fingerprint remains, users with no valid keys become disabled, and the repaired manifest is accepted

#### Scenario: Repair can unblock a user with only invalid entries

- **WHEN** a user contains only invalid key entries and the administrator runs manifest-wide invalid-key repair
- **THEN** the entries are removed, the user becomes disabled, and the user's effective authorized-key file is absent

#### Scenario: Invalid-key repair handles invalid entries in multiple users

- **WHEN** an administrator runs invalid-key repair while another user has invalid entries
- **THEN** the operation succeeds atomically, repairs both users, and preserves every valid key and enabled state that is not changed by the zero-valid-key rule

### Requirement: Generate an additional keypair

The Admin Users UI SHALL allow an administrator to generate an additional Ed25519 keypair for an existing user through an authenticated, CSRF-protected `POST /users/generate-key`. The generated public key SHALL be appended without replacing existing keys, and the private key SHALL be delivered using the existing single-use mechanism with a ten-minute TTL from generation. The private key SHALL NOT be stored in the manifest, persistent storage, or logs. Generating a key for a disabled user SHALL not re-enable that user. The download response SHALL use `Cache-Control: no-store`; unknown, expired, consumed, or post-restart tokens SHALL return `404`.

#### Scenario: Generate and download an additional key

- **WHEN** an authenticated administrator generates a key for existing user `alice`
- **THEN** a new Ed25519 public key is appended, the UI shows its fingerprint and a one-time private-key download, and `alice` can use the downloaded key after reconciliation

#### Scenario: Generated private key is single-use

- **WHEN** the administrator downloads the generated private key once
- **THEN** the first download succeeds and a second download using the same token is rejected

#### Scenario: Lost generated key can be revoked

- **WHEN** the administrator cannot recover a generated private key
- **THEN** the corresponding fingerprint can be removed through the key lifecycle operation without affecting the user's other keys

#### Scenario: Generating a key does not re-enable a disabled user

- **WHEN** an administrator generates an additional key for a disabled user
- **THEN** the new key is retained in the manifest but new SFTP logins remain rejected until the user is explicitly re-enabled

### Requirement: Define mutation response and security contracts

All state-changing Admin form endpoints, including `POST /users/add-key`, `/users/delete-key`, `/users/remove-invalid-keys`, `/users/generate-key`, and `/users/status`, SHALL use the existing authentication and CSRF funnel. Unauthenticated requests SHALL return `401`; missing or invalid CSRF tokens SHALL return `403` without mutation; malformed form fields, key material, or fingerprints SHALL return `400`; unknown users or user-scoped keys SHALL return `404`; strict-manifest conflicts and enable-with-zero-usable-keys SHALL return `409`; persistence or key-generation failures SHALL return `500`. Successful ordinary form mutations SHALL return `303` to `/`; successful generation SHALL return `200` with the one-time download page. Repeating a delete for an already absent fingerprint SHALL return `404` without mutation.

#### Scenario: Mutation without valid CSRF is rejected

- **WHEN** an authenticated administrator submits any state-changing endpoint without a valid CSRF token
- **THEN** the server returns `403`, leaves the manifest unchanged, and performs no reconciliation-triggering mutation
