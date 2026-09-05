## Why

The file-sharing listing uses a single `AdminPath + AdminPassword` gate: anyone with the link sees **all files** across all namespaces. With per-user directories in place, operators need per-audience visibility — Admin sees everything, each user sees `files/` plus only their own directory. A single shared secret path cannot express this.

## What Changes

- **Add a share-link management system to `file-sharing`**: Admin creates links with a scope (`admin` = all namespaces, or `user:<username>` = `files/` + that user's directory), an optional password, an optional expiry, and a description.
- **Serve scoped listings at `/l/:linkId`**: passwordless links render directly; password-protected links require a password form + HMAC-signed session cookie (24h). File lists are filtered by the link's scope. Downloads keep the existing `/<dirHash>/<fileHash>` route.
- **Add a management API** (`POST/GET/GET/PATCH/DELETE /api/links`) authenticated with a Bearer `ADMIN_TOKEN`, plus a JSON file-list endpoint per link.
- **Persist links** in `links.json` on a new named volume `file-links`, with atomic writes (temp file + rename).
- **Deprecate the old listing**: remove `ADMIN_PATH` / `ADMIN_PASSWORD` config, the `GET /<ADMIN_PATH>` handler, and the password-form listing. **BREAKING**: existing `ADMIN_PATH` URLs stop working; operators must create scoped links instead.
- **Phase 3 (sftp-admin UI "File Links" tab) is out of scope** — deferred to a follow-up change after the admin UI foundation lands.

## Capabilities

### New Capabilities
(none — the link system extends the existing file-sharing service rather than creating a new spec area)

### Modified Capabilities
- `file-sharing`: replace the single-secret listing model with scoped share links (link CRUD API, scoped access pages, expiry, password hashing). The old `ADMIN_PATH`/`ADMIN_PASSWORD` listing requirement is removed.

## Impact

- **`internal/server/server.go`** (file-sharing): new routes `/l/:linkId`, `/l/:linkId/auth`, `/l/:linkId/files`, `/api/links*`; remove `handleIndex` and the admin-password form flow.
- **New `internal/links/` package**: `Link` / `Scope` types, `LinkStore` (JSON file persistence with atomic writes), scope filtering against eligible namespaces.
- **New dependency `golang.org/x/crypto`** (bcrypt for link passwords) — `go.mod` currently has zero external dependencies.
- **`internal/config/config.go`**: remove `AdminPath` / `AdminPassword`; add `AdminToken` (required) and `LinksDir` (default `/var/lib/file-links`).
- **`docker-compose.yml`**: add named volume `file-links` mounted to `file-sharing`; add `ADMIN_TOKEN` env var; stop passing `ADMIN_PATH` / `ADMIN_PASSWORD` to `file-sharing`.
- **`.env.example`**: add `ADMIN_TOKEN`; mark `ADMIN_PATH` / `ADMIN_PASSWORD` as removed.
- **Coordination with `improve-admin-ui-and-settings`**: that change's `SharedConfig` and settings page cover only the surviving runtime settings (already trimmed — see that change's artifacts). **Order: implement this change FIRST** (it restructures `internal/server/server.go` and `internal/config/config.go`), **then `improve-admin-ui-and-settings`** (its per-request config overlay builds on the new structure). Do NOT implement in parallel — both touch `internal/server/server.go` and `docker-compose.yml`.
- **`openspec/specs/file-sharing/spec.md`**: delta replacing the "Link generation interface" requirement with link-management requirements.
