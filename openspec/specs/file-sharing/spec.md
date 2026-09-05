# File Sharing Specification

## Purpose

提供一個可由 Docker Compose 部署的檔案分享服務，將配置的檔案根目錄轉成不暴露主機絕對路徑的 hash URL，並安全地回應檔案下載。

## Requirements

### Requirement: Configurable deployment and logical sharing prefix

The deployment SHALL accept a configurable Host bind source through `HOST_SHARE_PATH` and mount it read-only at the fixed Container path `/opt/sharefiles`. The service SHALL use `SHARE_PREFIX` as the normalized SFTP-visible logical path prefix, and SHALL read the public URL, file hash target, and hash algorithm from configuration, using `file` and `md5` as defaults where applicable. `HOST_SHARE_PATH` SHALL never be included in a hash or exposed in an HTTP response. The public URL SHALL be normalized so generated links do not contain a duplicate trailing slash.

#### Scenario: Defaults are applied

- **WHEN** the service starts without optional configuration
- **THEN** it uses `/opt/sharefiles` as the Container share root, `file` as the file hash target, and `md5` as the hash algorithm

#### Scenario: Invalid configuration is rejected

- **WHEN** the configured logical prefix, hash target, or algorithm is invalid, or the mounted sharing root is unavailable
- **THEN** the service refuses to start and reports which configuration is invalid

### Requirement: Host path and SFTP logical path mapping

The service SHALL resolve logical paths from all eligible top-level directories under the container share root, not only the configured `SHARE_PREFIX`. The shared prefix directory (e.g. `files/`) SHALL remain the primary namespace for collaborative uploads. Additional top-level directories whose names match SFTP usernames SHALL form per-user namespaces. Each top-level directory produces logical paths prefixed with its directory name (e.g. `files/mydir/report.pdf` or `jonathan/docs/notes.pdf`). The mapping SHALL allow the Host absolute path to change without changing the hash for an unchanged logical SFTP path.

#### Scenario: Shared prefix directory resolves as before

- **WHEN** `HOST_SHARE_PATH` contains the configured logical prefix and the SFTP path is `files/mydir/cron.txt` with `SHARE_PREFIX=files`
- **THEN** the service resolves the file under `/opt/sharefiles/files/mydir/cron.txt` and computes the directory identity from `files/mydir`

#### Scenario: Per-user directory resolves as independent namespace

- **WHEN** a user uploads `jonathan/docs/notes.pdf` through SFTP
- **THEN** the web service resolves the file under `/opt/sharefiles/jonathan/docs/notes.pdf` and computes the directory identity from `jonathan/docs`

#### Scenario: Host path is not part of URL identity

- **WHEN** the same logical SFTP tree is mounted from two different Host absolute paths
- **THEN** the generated directory and file hashes are identical for the same logical path and file identity

#### Scenario: Logical prefix is outside the mounted tree

- **WHEN** `SHARE_PREFIX` is missing, absolute, contains parent traversal, or does not identify a usable directory beneath `/opt/sharefiles`
- **THEN** the service refuses to serve that namespace and reports an invalid or unhealthy configuration

### Requirement: Deterministic hash-based download URL

The service SHALL expose downloads using the route `/<directory-hash>/<file-hash>`. The directory hash SHALL be calculated from the normalized SFTP logical directory path, including `SHARE_PREFIX` and excluding all Host or Container absolute path components. The `file` target SHALL hash file contents while the `filename` target SHALL hash the file name. The configured algorithm SHALL be used for both hash components.

#### Scenario: Content-hash URL downloads a file

- **WHEN** a client requests a valid directory hash and the content hash of a regular file under that directory with `HASH_TARGET=file`
- **THEN** the service returns the file contents with a download-friendly response and does not reveal the host absolute path

#### Scenario: Filename-hash URL downloads a file

- **WHEN** a client requests a valid directory hash and the hash of a regular file name with `HASH_TARGET=filename`
- **THEN** the service returns that file with a download-friendly response

#### Scenario: Hash algorithm changes URL identifiers

- **WHEN** the same file is served once with `md5` and once with `sha256`
- **THEN** each configuration resolves only the corresponding algorithm's hash format

### Requirement: Link generation interface

The service SHALL serve the file listing only at the configured secret path `GET /<ADMIN_PATH>` when `ADMIN_PATH` is set; an empty `ADMIN_PATH` disables the listing entirely. All other index paths (including `/`) SHALL return 200 with an empty body, revealing nothing about the service or the shared files. When `ADMIN_PASSWORD` is set, the admin path SHALL require the submitted password (via the password form) before showing the listing; the secret path alone is then insufficient. The listing SHALL enumerate shareable regular files beneath the sharing root and display a complete link using the configured public URL, directory hash, and file hash. The interface SHALL indicate the active hash target and algorithm.

#### Scenario: User obtains a share link

- **WHEN** a user opens the service's file listing and the sharing root contains a regular file
- **THEN** the page shows the file and a copyable URL matching `PUBLIC_URL/<directory-hash>/<file-hash>`

#### Scenario: Listing is hidden without the secret path

- **WHEN** `ADMIN_PATH` is set and a user opens `/` or any path other than `/<ADMIN_PATH>`
- **THEN** the service returns 200 with an empty body and no file information

#### Scenario: Listing is disabled without configuration

- **WHEN** `ADMIN_PATH` is empty and a user opens any index path
- **THEN** the service returns 200 with an empty body and no file information

#### Scenario: Password gate protects the listing

- **WHEN** `ADMIN_PASSWORD` is set and a user opens `/<ADMIN_PATH>` without the password or with a wrong password
- **THEN** the service returns 401 with a password form and no file information
- **WHEN** the user submits the correct password
- **THEN** the service shows the file listing

#### Scenario: Unavailable file is omitted or marked unavailable

- **WHEN** a listed file is removed or becomes unreadable before link generation
- **THEN** the interface does not emit a link that the download endpoint cannot resolve and reports the file as unavailable

### Requirement: Safe filesystem boundary

The service SHALL resolve all requests within the configured logical namespaces below `/opt/sharefiles/`, reject path traversal and invalid URL components, refuse non-regular files, and refuse symlinks whose resolved target escapes any eligible namespace. The eligible namespaces SHALL be the shared prefix directory and any per-user directories present under the container share root. The service SHALL treat the mounted sharing root as read-only from its perspective and SHALL NOT serve files from directories that do not correspond to the shared prefix or a valid SFTP username.

#### Scenario: Traversal attempt is rejected

- **WHEN** a client sends a URL or encoded path that attempts to reach a parent directory or an absolute filesystem path
- **THEN** the service returns a client error or not-found response and does not access a file outside any eligible namespace

#### Scenario: Escaping symlink is rejected

- **WHEN** a hash resolves to a symlink whose target is outside all eligible namespaces
- **THEN** the service rejects the request and does not return the target contents

#### Scenario: Directory target is rejected

- **WHEN** a hash resolves to a directory, device, socket, or other non-regular file
- **THEN** the service returns a not-found or unsupported-target response

#### Scenario: Unknown top-level directory is not served

- **WHEN** a directory exists under `/opt/sharefiles/` that is neither the shared prefix nor a valid SFTP username
- **THEN** the service SHALL NOT include files from that directory in scan results or serve them via hash URL

### Requirement: Explicit HTTP outcomes

The download endpoint SHALL return not-found for unknown hash components, an appropriate client error for malformed routes, and a server error for unexpected filesystem failures. Hash collisions that make a request resolve to multiple files SHALL not result in an arbitrary file being served.

#### Scenario: Unknown link is requested

- **WHEN** a client requests a syntactically valid but unknown directory or file hash
- **THEN** the service returns HTTP 404 without exposing filesystem details

#### Scenario: Hash collision is detected

- **WHEN** a requested hash maps to more than one eligible file in its directory
- **THEN** the service returns a deterministic conflict or unavailable response and serves none of the colliding files

### Requirement: Service health and container operation

The service SHALL expose a health endpoint suitable for Docker health checks and SHALL run as a non-root process in an Alpine-based container. The Compose definition SHALL mount the configured sharing root read-only and publish the configured HTTP port.

#### Scenario: Healthy container is reported

- **WHEN** the service has started and can access its configured sharing root
- **THEN** the health endpoint returns a successful status suitable for a container health check

#### Scenario: Missing share mount is detected

- **WHEN** the container starts without a usable sharing-root mount
- **THEN** startup or health checking reports an unhealthy service instead of serving from an unintended directory
