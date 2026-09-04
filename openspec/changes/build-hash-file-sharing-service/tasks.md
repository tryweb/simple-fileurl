## 1. Service foundation

- [x] 1.1 Create the Go module and package layout for configuration, hashing, filesystem indexing, HTTP handlers, and application startup; verify `go test ./...` discovers and builds all packages.
- [x] 1.2 Implement service configuration for fixed Container root `/opt/sharefiles`, configurable `SHARE_PREFIX`, `PUBLIC_URL`, `HASH_TARGET`, `HASH_ALGORITHM`, and `PORT`; verify unit tests cover valid prefixes, traversal or absolute prefixes, unsupported values, missing root, and normalized public URLs.

## 2. Hashing and safe file resolution

- [x] 2.1 Implement canonical SFTP logical-directory and file identity hashing for `file` and `filename` targets with MD5 and SHA-256; verify tests assert exact lowercase hexadecimal hashes, inclusion of `SHARE_PREFIX`, and exclusion of Host/Container absolute paths.
- [x] 2.2 Implement a filesystem scanner and resolver that maps `SHARE_PREFIX` below `/opt/sharefiles`, indexes regular files, excludes symlinks and non-regular files, detects hash collisions, and enforces the configured namespace boundary; verify tests cover nested logical paths, traversal input, escaping symlinks, missing files, and collisions.
- [x] 2.3 Implement `GET /<directory-hash>/<file-hash>` download handling with streamed responses, safe content headers, and explicit 404/409/5xx outcomes; verify HTTP tests cover successful downloads, malformed routes, unknown hashes, collisions, and filesystem failures without leaking absolute paths.

## 3. User-facing HTTP behavior

- [x] 3.1 Implement the root HTML listing page with escaped SFTP logical paths, active hash configuration, and copyable links built from `PUBLIC_URL`; verify handler tests cover configurable prefixes, nested files, empty directories, unreadable or removed files, and HTML escaping.
- [x] 3.2 Implement `/healthz` and application startup checks for a usable `/opt/sharefiles/SHARE_PREFIX` namespace, non-mutating read-only operation, and graceful server errors; verify tests assert healthy and missing-prefix responses and that startup does not create files in the share directory.

## 4. Container and Compose deployment

- [x] 4.1 Add a multi-stage Alpine Dockerfile that runs the compiled service as a non-root user with required certificates; verify `docker build` succeeds and a container inspection confirms the runtime user is not root.
- [x] 4.2 Add Docker Compose and `.env.example` configuration for `HOST_SHARE_PATH` to fixed container `/opt/sharefiles:ro`, configurable `SHARE_PREFIX`, public URL, hash settings, port publishing, and `/healthz` healthcheck; verify `docker compose config` renders the expected read-only mount and does not pass the Host path into hash configuration.
- [x] 4.3 Add deployment documentation with startup, configuration, logical-to-physical path mapping examples, URL examples, bearer-link warning, reverse-proxy/TLS placement, and rollback steps; verify the documented commands and environment names match the Compose definition and do not present observed Host paths as defaults.

## 5. End-to-end verification

- [x] 5.1 Add an integration fixture and HTTP end-to-end tests that map a configurable Host source to `/opt/sharefiles/SHARE_PREFIX`, generate links from logical SFTP paths, and download files using both hash targets and both algorithms; verify the full test suite passes with no absolute-path disclosure.
- [x] 5.2 Run the built Compose service against a temporary share directory and verify `/healthz`, `/`, a generated download URL, unknown-hash 404 behavior, and container shutdown; record the smoke-test command and successful results.
