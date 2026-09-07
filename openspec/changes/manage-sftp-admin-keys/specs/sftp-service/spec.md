## MODIFIED Requirements

### Requirement: Runtime user reconciliation

The SFTP service SHALL read an atomic user manifest from the shared `sftp-users` volume and SHALL reconcile enabled users into local Linux accounts, per-user authorized-key files, and per-user writable directories without restarting the container. When a new user is enabled, the service SHALL create `/share/<username>/` owned by `<username>` with mode `0700`. When a user is disabled or removed, the service SHALL leave the per-user directory and its contents intact on disk but the user will no longer be able to access them via SFTP. The manifest SHALL contain usernames matching `^[a-z_][a-z0-9_-]{0,31}$`, an `enabled` flag, and either zero or more valid SSH public keys. An enabled user with `authorized_keys: []` is an intentional empty authorization set and SHALL have no effective authorized key. Authorized-key files SHALL be outside the chroot, owned by `root:sftpusers`, mode `0640`, and not writable by SFTP users. Disabled users SHALL have no effective authorized key and SHALL be unable to start a new SFTP session. Malformed JSON, missing required fields, wrong field types, or invalid key material SHALL preserve the prior effective state and return non-zero rather than silently revoking or broadening access.

#### Scenario: Admin-created user gets personal directory

- **WHEN** the admin atomically adds enabled user `jonathan` and a valid public key to the manifest
- **THEN** the SFTP container creates or updates `jonathan`, its authorized-key file, and a `/share/jonathan/` directory owned by `jonathan` with mode `0700`, all without restart

#### Scenario: Revoked user loses SFTP access but directory persists

- **WHEN** the admin marks `jonathan` disabled
- **THEN** the SFTP container removes or invalidates `jonathan`'s effective key without restart, subsequent login attempts fail, and `/share/jonathan/` remains on disk with its contents

#### Scenario: Per-user directory creation is idempotent

- **WHEN** the reconcile loop runs multiple times for the same enabled user
- **THEN** the per-user directory is created only once and subsequent runs do not alter its ownership or permissions

#### Scenario: Valid empty authorization revokes stale access

- **WHEN** the reconciler reads a well-formed enabled user with `authorized_keys: []` while a stale effective authorized-key file exists
- **THEN** it removes the effective file, exits successfully, and does not permit a new SFTP login for that user

#### Scenario: Malformed authorization input preserves fail-safe state

- **WHEN** the reconciler reads malformed JSON, a manifest with missing or incorrectly typed fields, or an invalid key entry while an effective authorized-key file exists
- **THEN** it leaves the prior effective file unchanged, exits non-zero, and does not create or broaden authorization

#### Scenario: Empty authorization remains safe across repeated reconciliation

- **WHEN** the reconciler processes the same valid empty authorization set more than once
- **THEN** the effective authorized-key file remains absent and each reconciliation succeeds without changing unrelated users or directories
