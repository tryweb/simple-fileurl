## 1. Configuration

- [x] 1.1 Add `WEB_GID` field to `internal/config/config.go` with env var `WEB_GID`, default `2001`, and validation (must be a numeric GID not equal to `SFTP_GID`). Verify `TestLoadValid` and new `TestWebGIDValidation` pass.
- [x] 1.2 Add `WEB_GID` to `.env.example` and `docker-compose.yml` environment for both `sftp` and `file-sharing` services. Verify `docker compose config` parses without errors.

## 2. SFTP Reconcile — Per-User Directory Creation

- [x] 2.1 Modify `sftp/reconcile.sh` to create `/share/<username>/` directory (mode `0750`, owner `<username>`, group `WEB_GID`) when an enabled user is reconciled. Use `WEB_GID` env var (default `2001`). Verify: create a test user, confirm directory exists with correct ownership and permissions.
- [x] 2.2 Ensure per-user directory creation is idempotent in `reconcile.sh` — re-running reconcile for the same user does not alter existing directory ownership or permissions. Verify: run reconcile twice, confirm no permission changes on second run.
- [x] 2.3 Add `ensure_web_group` function to `sftp/reconcile.sh` (or `entrypoint.sh`) that creates the `WEB_GID` group if it does not exist, similar to `ensure_group` for `SFTP_GID`. Verify: container starts cleanly with default `WEB_GID=2001`.

## 3. SFTP Entrypoint — Validation

- [x] 3.1 Update `sftp/entrypoint.sh` `validate_share` to accept `WEB_GID` env var and validate it is a numeric GID. Verify: `entrypoint.sh check` passes with valid `WEB_GID`, fails with invalid value.
- [x] 3.2 Update `sftp/entrypoint.sh` to ensure the web readers group exists before reconcile loop starts. Verify: group is created on container start.

## 4. Web Service — Multi-Namespace Scanning

- [x] 4.1 Refactor `internal/config/config.go` `NamespaceRoot()` to return the container share root (`/opt/sharefiles`) instead of the single prefix path. Add a helper `IsEligibleNamespace(name string) bool` that returns true for `SHARE_PREFIX` or names matching the SFTP username pattern. Verify existing `NamespaceRoot` callers compile.
- [x] 4.2 Update `internal/store/store.go` `scan()` to iterate over all eligible top-level directories under the container share root. For each directory, construct logical paths using the directory name as prefix (e.g. `files/mydir/file.txt` or `jonathan/docs/notes.pdf`). Verify: `TestListLogicalPaths` updated and new test `TestPerUserDirectoryListed` passes.
- [x] 4.3 Update `internal/store/store.go` `openPath()` to validate resolved files are within any eligible namespace (not just the single prefix). Verify: `TestOutsideNamespaceNotServed` updated to confirm ineligible directories are excluded.
- [x] 4.4 Update `internal/config/config.go` validation: the container share root must exist and contain at least the `SHARE_PREFIX` directory. Per-user directories are optional. Verify: startup fails if `SHARE_PREFIX` directory is missing, succeeds if only `SHARE_PREFIX` exists (no per-user dirs).

## 5. Docker Compose — Web Container Group Membership

- [x] 5.1 Update `Dockerfile` (file-sharing) to accept `WEB_GID` build arg and ensure the container's non-root user is a member of that GID. Alternatively, use `supplementary_groups` in `docker-compose.yml`. Verify: web process inside container has the `WEB_GID` group in `id` output.
- [x] 5.2 Update `docker-compose.yml` `file-sharing` service to pass `WEB_GID` env var and configure group membership. Verify: `docker compose up` starts successfully and web process can read files in a per-user directory with mode `0750` and group `WEB_GID`.

## 6. Integration Testing

- [x] 6.1 Add integration test: create two SFTP users (`alice`, `jonathan`), upload files to their per-user directories and to `files/`, verify web service serves all files with correct hash URLs. Verify: `curl` to each hash URL returns the correct file.
- [x] 6.2 Add integration test: verify SFTP user `alice` cannot access `/share/jonathan/` (permission denied). Verify: `sftp` client attempt to `ls /share/jonathan/` fails.
- [x] 6.3 Add integration test: verify web service does NOT serve files from a directory that is neither `SHARE_PREFIX` nor a valid SFTP username (e.g. a directory named `temp-data/` manually created under `HOST_SHARE_PATH`). Verify: files in `temp-data/` do not appear in scan results.
- [x] 6.4 Run full existing test suite (`go test ./...` and SFTP container tests) to confirm no regressions. Verify: all tests pass.
