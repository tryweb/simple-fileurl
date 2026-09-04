# sftp-service Specification

## Purpose

為部署提供與 web 分享服務共用同一 Host 目錄的 SFTP 上傳／管理入口，讓檔案上傳後可直接透過既有 hash URL 分享下載。

## Requirements

### Requirement: Shared data root between SFTP and web services

The deployment SHALL mount the same Host bind source (`HOST_SHARE_PATH`) into both the `file-sharing` service (read-only, at `/opt/sharefiles`) and the `sftp` service (read-write, at `/share`). Files written via SFTP under `/share/SHARE_PREFIX` SHALL become visible to the web service under the same logical SFTP path with no additional sync step.

#### Scenario: Upload via SFTP is downloadable via hash URL

- **WHEN** a user uploads `files/mydir/report.pdf` through SFTP
- **THEN** the web service serves it at the hash URL derived from the same logical path and file identity

#### Scenario: Web service mount stays read-only

- **WHEN** the deployment is inspected via `docker compose config`
- **THEN** the `file-sharing` service still mounts the share with the `ro` flag while the `sftp` service mounts it read-write

### Requirement: Root-owned chroot and constrained write scope

The SFTP service SHALL use `/share` as the chroot root. `/share` SHALL be owned by `root:root` and SHALL NOT be writable by the SFTP users; the configured `/share/SHARE_PREFIX` directory SHALL be the only writable data subtree. The deployment SHALL refuse to serve when the chroot root is missing, writable by an unprivileged user, or not a directory.

#### Scenario: Chroot ownership is valid

- **WHEN** the SFTP container starts with a valid share root
- **THEN** `stat` reports `/share` as a root-owned non-writable directory and SFTP sessions are confined below it

#### Scenario: Invalid chroot ownership fails fast

- **WHEN** `/share` is writable by an unprivileged user or cannot be used as a chroot
- **THEN** the SFTP container refuses to serve and logs the invalid share-root condition

#### Scenario: Writes are limited to the prefix

- **WHEN** a user uploads or creates a path outside `/share/SHARE_PREFIX`
- **THEN** the operation is rejected and no data outside the configured prefix is changed

### Requirement: Public-key-only authentication

The SFTP service SHALL accept public-key authentication and SHALL reject password and keyboard-interactive authentication. The set of users and authorized keys SHALL be managed through the admin UI (see `sftp-admin`) and optionally seeded from deployment configuration on first start. The SFTP service SHALL NOT depend on the admin password to start.

#### Scenario: Key login succeeds

- **WHEN** a client connects with an admin-provisioned username and a matching private key
- **THEN** the login succeeds and the session is confined to the share root

#### Scenario: Password login is rejected

- **WHEN** a client attempts password or keyboard-interactive authentication
- **THEN** the server rejects it regardless of the credential supplied

#### Scenario: Share configuration fails fast

- **WHEN** the deployment starts without a usable share root or required SFTP configuration
- **THEN** the SFTP container refuses to serve and logs which SFTP configuration is missing

### Requirement: Runtime user reconciliation

The SFTP service SHALL read an atomic user manifest from the shared `sftp-users` volume and SHALL reconcile enabled users into local Linux accounts and per-user authorized-key files without restarting the container. The manifest SHALL contain usernames matching `^[a-z_][a-z0-9_-]{0,31}$`, an `enabled` flag, and one or more valid SSH public keys. Authorized-key files SHALL be outside the chroot, owned by `root:sftpusers`, mode `0640`, and not writable by SFTP users. Disabled users SHALL have no effective authorized key and SHALL be unable to start a new SFTP session.

#### Scenario: Admin-created user is reconciled

- **WHEN** the admin atomically adds enabled user `alice` and a valid public key to the manifest
- **THEN** the SFTP container creates or updates `alice` and its authorized-key file without restart, and `alice` can log in

#### Scenario: Revoked user is reconciled

- **WHEN** the admin marks `alice` disabled
- **THEN** the SFTP container removes or invalidates `alice`'s effective key without restart and subsequent login attempts fail

### Requirement: Chrooted visible tree and restricted session

The SFTP service SHALL confine each session to `/share`, expose the configured `SHARE_PREFIX` subtree at the expected logical path, disable shell, PTY, agent forwarding, TCP forwarding, and X11 forwarding, and reject path traversal outside the chroot.

#### Scenario: Session root matches share tree

- **WHEN** a user lists the top level of an SFTP session with `SHARE_PREFIX=files`
- **THEN** they see the `files` subtree corresponding to `/opt/sharefiles/files` and no Host absolute path

#### Scenario: Traversal is confined

- **WHEN** a user attempts `cd ..` above the chroot or requests `/etc/hostname`
- **THEN** the operation is rejected or resolves only within `/share`, and the Host filesystem is not exposed

### Requirement: Web-readable uploaded files

The SFTP service SHALL create uploaded regular files with permissions readable by the non-root `file-sharing` process and directories traversable by that process. The default upload policy SHALL use a fixed `SFTP_GID`, a setgid writable prefix directory, `umask 0002`, regular-file mode equivalent to `0664`, and directory mode equivalent to `0775`.

#### Scenario: Uploaded file is readable by web service

- **WHEN** a user uploads a regular file under `SHARE_PREFIX`
- **THEN** the web service can read the file through its read-only mount and its hash URL returns HTTP 200 with identical bytes without a restart

#### Scenario: Permission contract is rejected

- **WHEN** the prefix directory cannot be written by the SFTP group or existing files are not readable by the web process
- **THEN** deployment validation reports the permission problem instead of silently accepting uploads that web cannot serve

### Requirement: Configurable SFTP port

The deployment SHALL allow configuring the Host-side published SFTP port (default 2222) through environment variables. The SFTP port SHALL NOT clash with the web service port by default. Login usernames are managed via the admin UI and manifest, not environment variables.

#### Scenario: Default port mapping

- **WHEN** the deployment starts without SFTP port overrides
- **THEN** the SFTP service is reachable on the Host at the documented default port

#### Scenario: Custom port

- **WHEN** the deployer sets the SFTP port variable
- **THEN** the service listens on the custom port
