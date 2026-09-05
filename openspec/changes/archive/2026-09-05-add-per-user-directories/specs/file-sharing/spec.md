## MODIFIED Requirements

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
