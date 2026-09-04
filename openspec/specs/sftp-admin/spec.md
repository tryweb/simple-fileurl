# sftp-admin Specification

## Purpose

為不熟悉命令列的管理者提供 Web 介面，用來建立與停用 SFTP 使用者、產生 SSH keypair 或登記外部公鑰，讓 SFTP 上傳入口真正可用。

## Requirements

### Requirement: Password-protected admin interface

The `sftp-admin` service SHALL serve its UI over HTTP on a dedicated Host-side port, bind to localhost by default, and SHALL require `SFTP_ADMIN_PASSWORD` before showing or mutating anything. Unauthenticated requests to management endpoints SHALL be rejected. The service SHALL fail fast when the password is missing, and production deployment documentation SHALL require TLS or a private network before exposing the UI beyond localhost.

#### Scenario: Login gate

- **WHEN** a client opens the admin UI without a valid admin session
- **THEN** it sees only a password prompt and no user or key data

#### Scenario: Wrong password is rejected

- **WHEN** a client submits an incorrect admin password
- **THEN** access is denied and no session is created

#### Scenario: Missing admin password fails fast

- **WHEN** the admin container starts without `SFTP_ADMIN_PASSWORD`
- **THEN** it exits non-zero and logs the missing configuration without serving management endpoints

### Requirement: Bounded authenticated admin sessions

After successful login, the admin UI SHALL issue a random opaque HttpOnly session cookie with SameSite protection and a maximum lifetime of 30 minutes. It SHALL provide logout, compare passwords in constant time, rate-limit repeated failed logins, and require a CSRF token on state-changing browser requests.

#### Scenario: Session expires

- **WHEN** an authenticated session is older than 30 minutes or the admin logs out
- **THEN** subsequent management requests require login again

#### Scenario: Brute-force attempts are throttled

- **WHEN** a client repeatedly submits incorrect admin passwords
- **THEN** later attempts are delayed or rejected according to the documented rate limit

### Requirement: Create SFTP user with generated keypair

The admin UI SHALL allow creating an SFTP user by a username matching `^[a-z_][a-z0-9_-]{0,31}$` and generating an `ed25519` keypair for that user. The private key SHALL be delivered through a single-use download response, SHALL NOT be stored in the user manifest, persistent storage, or logs, and SHALL NOT be retrievable afterwards; only the public key is retained. The new user SHALL be able to log in via the reconciled SFTP service. Reserved system names such as `root`, `admin`, `sshd`, and `sftpusers` SHALL be rejected.

#### Scenario: Admin creates user and hands over private key

- **WHEN** the admin creates user `alice` with a generated keypair
- **THEN** the UI offers the private key for one-time download, stores only the public key, and `alice` can SFTP-login with the downloaded key

#### Scenario: Private key is not recoverable

- **WHEN** the admin re-opens an existing user's page
- **THEN** the private key is no longer shown or downloadable

#### Scenario: Duplicate username is rejected

- **WHEN** the admin creates a user with an existing username
- **THEN** the request fails with a clear error and no key material is changed

#### Scenario: Reserved username is rejected

- **WHEN** the admin submits `root` or another reserved system username
- **THEN** the request fails and no local account or key is created

### Requirement: Register external public key

The admin UI SHALL allow registering an externally generated public key (e.g. from the user's own `ssh-keygen`) for either a new or existing SFTP user. It SHALL validate the key type and format, display its fingerprint, and append it idempotently to the user's authorized-key set rather than silently replacing other keys. Invalid key material SHALL be rejected with a clear error.

#### Scenario: Register own public key

- **WHEN** the admin pastes a valid `ssh-ed25519` public key for user `bob`
- **THEN** `bob` can SFTP-login with the matching private key

#### Scenario: Malformed key is rejected

- **WHEN** the admin submits text that is not a valid SSH public key
- **THEN** the request fails and nothing is stored

#### Scenario: Multiple keys are preserved

- **WHEN** the admin registers a second distinct key for an existing user
- **THEN** both keys remain effective and submitting the same key again does not create a duplicate

### Requirement: List and revoke SFTP users

The admin UI SHALL list all SFTP users and SHALL allow revoking (disabling or deleting) a user. Revocation SHALL be written atomically to the shared manifest and SHALL remove or invalidate the user's effective authorized keys. After revocation, that user's new SFTP login attempts SHALL be rejected while other users are unaffected.

#### Scenario: Revoked user cannot log in

- **WHEN** the admin revokes user `alice`
- **THEN** `alice`'s subsequent SFTP authentication fails and other users can still log in

### Requirement: User manifest is the only cross-container control plane

The admin UI SHALL write only the shared `sftp-users` manifest and SHALL NOT require access to the SFTP container's `/etc/passwd`, `/etc/ssh`, or share data mount. Manifest writes SHALL use a temporary file followed by an atomic rename, and the SFTP service SHALL reconcile the result without an admin-triggered container restart.

#### Scenario: Atomic manifest update

- **WHEN** the admin creates, updates, or revokes a user
- **THEN** readers observe either the previous complete manifest or the next complete manifest, never a partial document
