## 1. Config & Storage Foundation

- [x] 1.1 Update `internal/config/config.go`: remove `AdminPath` / `AdminPassword` (+ `ValidateAdminPath`), add `AdminToken` (required at startup, fail fast when empty; runtime-rotatable via the shared config, which invalidates outstanding tokens and link sessions) and `LinksDir` (default `/var/lib/file-links`). Update `config_test.go` for the new fields and the missing-token failure. Verify `go test ./internal/config/` passes.
- [x] 1.2 Add `golang.org/x/crypto` to `go.mod` for bcrypt and verify the module builds (`go build ./...`).
- [x] 1.3 Create `internal/links/links.go` with `Link`, `Scope` (`admin` / `user`), and `LinkStore` (mutex-guarded map, JSON load/save with write-to-temp + rename, lazy purge of expired entries on write). Add unit tests for CRUD, atomic save/load round-trip, and concurrent writes. Verify tests pass.

## 2. Management API

- [x] 2.1 Add `POST /api/links` and `GET /api/links` handlers: Bearer token auth (constant-time compare), scope validation (`user:<name>` must match the SFTP username pattern), bcrypt-hash the password when set, never return password hashes. Verify with unit tests (valid/invalid token, valid/invalid scope, password flag in list output).
- [x] 2.2 Add `GET /api/links/:id`, `PATCH /api/links/:id`, `DELETE /api/links/:id` handlers: password rotation, expiry/description update, delete returns 204 and the link becomes unreachable. Verify with unit tests including 404 for unknown IDs.

## 3. Scoped Access Pages

- [x] 3.1 Add `GET /l/:linkId` handler: render the file list directly for passwordless links; render a password form for protected links; return 404 for unknown or expired IDs. Filter files by scope (`admin` = all eligible namespaces, `user:<name>` = `files/` + `<name>/`). Each entry shows logical path, size, and the existing `/<dirHash>/<fileHash>` download URL. Verify with unit tests covering scope isolation (no cross-user leakage).
- [x] 3.2 Add `POST /l/:linkId/auth` handler: bcrypt-compare the submitted password, set an HMAC-signed HttpOnly SameSite session cookie (24h, keyed by `ADMIN_TOKEN`), redirect to the listing; wrong password returns 401. Verify with unit tests (correct/wrong password, cookie set, expired session rejected).
- [x] 3.3 Add `GET /l/:linkId/files` JSON endpoint returning link metadata + filtered file list (logical path, dir/file hashes, size, URL). Require the session cookie for password-protected links. Verify with unit tests.

## 4. Remove Old Listing

- [x] 4.1 Remove `handleIndex`, the `GET /<ADMIN_PATH>` route, and the admin-password form flow from `internal/server/server.go`. All index paths other than `/l/*` return 200 with an empty body (preserve the no-information-disclosure behavior). Verify `go test ./...` passes and no references to `AdminPath` remain (`grep -r AdminPath internal/ cmd/` returns nothing).
- [x] 4.2 Remove `ADMIN_PATH` / `ADMIN_PASSWORD` from `docker-compose.yml`, `docker-compose.dev.yml`, and `docker-compose.release.yml` (file-sharing environment); remove from `.env.example`; add `ADMIN_TOKEN` (required) and `LINKS_DIR` plus the `file-links` volume mount to all three compose files. Verify each parses (`docker compose -f <file> config` exits 0).
- [x] 4.3 Update `install.sh`: remove `ADMIN_PATH` from `required_values_present`, the `prompt_value ADMIN_PATH` line, and `validate_release_values`; add `prompt_secret ADMIN_TOKEN` plus a required-value check. Verify `bash -n install.sh` passes and a dry run generates a `.env` containing `ADMIN_TOKEN` with no `ADMIN_PATH`.
- [x] 4.4 Update `test/smoke.sh` to exercise the link flow (create a link via `POST /api/links`, open `/l/:linkId`, download a file) instead of curling `/${ADMIN_PATH}`; update `.github/workflows/ci.yml` to set `ADMIN_TOKEN` instead of `ADMIN_PATH`; update the `ADMIN_PATH` command examples in `.opencode/skills/release/SKILL.md`. Verify the smoke script passes locally and the workflow YAML parses.
- [x] 4.5 Update user-facing docs (`README.md`, `docs/usage.md`, `docs/installation-upgrade.md`, `docs/architecture.md`, `docs/development-testing.md`) wherever the secret-path listing is described; leave `docs/knowledge/` historical records untouched. Verify no stale listing references remain outside `docs/knowledge/` and the archived changes.

## 5. Docker & Deployment

- [x] 5.1 Add named volume `file-links` to `docker-compose.yml`, mounted to `file-sharing` at `/var/lib/file-links:rw`. Preserve existing `WEB_GID`, `group_add`, and share mounts. Verify `docker compose config` shows the mount.
- [x] 5.2 Document the migration in `docs/installation-upgrade.md` with three points: old `ADMIN_PATH` URLs stop working on deploy, how to create replacement scoped links via `POST /api/links` before upgrading, and the new required `ADMIN_TOKEN` variable. No automated migration — operators recreate links via the new API. Verify the section exists with all three points.

## 6. Integration & Verification

- [x] 6.1 Run all unit tests (`go test ./...`) with no regressions; new `internal/links` and handler tests cover link CRUD, scope isolation, password auth, and expiry.
- [x] 6.2 Build the image and run `docker compose up` locally. Verify end-to-end: create an `admin` link and a `user:<name>` link via the API, open both listings in a browser (password flow included), confirm the user link excludes other users' directories, delete a link and confirm 404, restart the container and confirm links persist.
- [x] 6.3 Verify health checks still pass and the download route `/<dirHash>/<fileHash>` is unaffected by the listing removal.
