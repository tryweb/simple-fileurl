## MODIFIED Requirements

### Requirement: Root-owned chroot and constrained write scope

The SFTP service SHALL use `/share` as the chroot root. `/share` SHALL be owned by `root:root` and SHALL NOT be writable by the SFTP users. The configured `/share/SHARE_PREFIX` directory SHALL be the shared writable subtree accessible by all SFTP users via group permissions. Additionally, the SFTP service SHALL provide a per-user writable directory at `/share/<username>/` for each enabled user. The per-user directory SHALL be owned by the user and SHALL NOT be writable or readable by other SFTP users. The deployment SHALL refuse to serve when the chroot root is missing, writable by an unprivileged user, or not a directory.

#### Scenario: Chroot ownership is valid

- **WHEN** the SFTP container starts with a valid share root
- **THEN** `stat` reports `/share` as a root-owned non-writable directory and SFTP sessions are confined below it

#### Scenario: Invalid chroot ownership fails fast

- **WHEN** `/share` is writable by an unprivileged user or cannot be used as a chroot
- **THEN** the SFTP container refuses to serve and logs the invalid share-root condition

#### Scenario: Shared prefix remains group-writable

- **WHEN** a user uploads or modifies a file under `/share/SHARE_PREFIX`
- **THEN** the operation succeeds and other SFTP users can also access the shared prefix directory

#### Scenario: Per-user directory is isolated

- **WHEN** user `jonathan` uploads a file under `/share/jonathan/`
- **THEN** the file is accessible only to `jonathan` and other SFTP users cannot read, write, or list the contents of `/share/jonathan/`

#### Scenario: Per-user directory prevents cross-user access

- **WHEN** user `alice` attempts to list or access `/share/jonathan/`
- **THEN** the operation is denied with a permission error

### Requirement: Runtime user reconciliation

The SFTP service SHALL read an atomic user manifest from the shared `sftp-users` volume and SHALL reconcile enabled users into local Linux accounts, per-user authorized-key files, and per-user writable directories without restarting the container. When a new user is enabled, the service SHALL create `/share/<username>/` owned by `<username>` with mode `0700`. When a user is disabled or removed, the service SHALL leave the per-user directory and its contents intact on disk but the user will no longer be able to access them via SFTP. The manifest SHALL contain usernames matching `^[a-z_][a-z0-9_-]{0,31}$`, an `enabled` flag, and one or more valid SSH public keys. Authorized-key files SHALL be outside the chroot, owned by `root:sftpusers`, mode `0640`, and not writable by SFTP users. Disabled users SHALL have no effective authorized key and SHALL be unable to start a new SFTP session.

#### Scenario: Admin-created user gets personal directory

- **WHEN** the admin atomically adds enabled user `jonathan` and a valid public key to the manifest
- **THEN** the SFTP container creates or updates `jonathan`, its authorized-key file, and a `/share/jonathan/` directory owned by `jonathan` with mode `0700`, all without restart

#### Scenario: Revoked user loses SFTP access but directory persists

- **WHEN** the admin marks `jonathan` disabled
- **THEN** the SFTP container removes or invalidates `jonathan`'s effective key without restart, subsequent login attempts fail, and `/share/jonathan/` remains on disk with its contents

#### Scenario: Per-user directory creation is idempotent

- **WHEN** the reconcile loop runs multiple times for the same enabled user
- **THEN** the per-user directory is created only once and subsequent runs do not alter its ownership or permissions

### Requirement: Web-readable uploaded files

The SFTP service SHALL create uploaded regular files with permissions readable by the non-root `file-sharing` process and directories traversable by that process. The shared `SHARE_PREFIX` subtree SHALL use a fixed `SFTP_GID`, a setgid writable prefix directory, `umask 0002`, regular-file mode equivalent to `0664`, and directory mode equivalent to `0775`. Per-user directories SHALL be owned by the user with mode `0700`; files within per-user directories SHALL be readable by the owner and the web service process via group membership or other appropriate permissions that allow the non-root web process to read and serve the files.

#### Scenario: Shared prefix file is readable by web service

- **WHEN** a user uploads a regular file under `SHARE_PREFIX`
- **THEN** the web service can read the file through its read-only mount and its hash URL returns HTTP 200 with identical bytes without a restart

#### Scenario: Per-user file is readable by web service

- **WHEN** a user uploads a regular file under their per-user directory
- **THEN** the web service can read the file through its read-only mount and its hash URL returns HTTP 200 with identical bytes without a restart

#### Scenario: Permission contract is rejected

- **WHEN** the prefix directory cannot be written by the SFTP group, per-user directories are not readable by the web process, or existing files are not readable by the web process
- **THEN** deployment validation reports the permission problem instead of silently accepting uploads that web cannot serve
