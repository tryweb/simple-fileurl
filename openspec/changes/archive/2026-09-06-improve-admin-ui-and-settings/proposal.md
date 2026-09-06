## Why

The current SFTP admin UI is a bare-bones HTML page with no styling, no layout structure, and only SFTP user management. Operators need a polished interface that also lets them manage the file-sharing service's runtime configuration (hash algorithm, hash target, public URL, passwords) without SSH-ing into the host. (The old `ADMIN_PATH` / `ADMIN_PASSWORD` listing model is removed by `file-share-link-management`; this change covers only the surviving runtime settings.)

## What Changes

- **Redesign the admin UI** with a proper layout: top navigation bar, card-based sections, responsive CSS, and visual hierarchy. Replace the current unstyled `<h1>`/`<table>`/`<form>` dump with a structured dashboard.
- **Add a Settings page** to the admin UI that lets operators view and modify configuration values through form inputs, with validation. All changes take effect immediately — no container restarts required.
- **Persist settings changes** to a shared config file (`config.json`) on a named volume mounted by both services. The `file-sharing` service reads this file on each request instead of using env vars.
- **Add navigation** between the existing Users page and the new Settings page via a top nav bar.

## Capabilities

### New Capabilities
- `admin-settings`: Settings management page in the admin UI — view/edit config values, validation, live updates without restart.

### Modified Capabilities
- `sftp-admin`: UI overhaul — add navigation, layout, and styling to the existing user management pages. The functional requirements (auth, user CRUD, key management) remain unchanged; only the presentation layer changes.

## Impact

- **`internal/sftpadmin/ui.go`**: Complete rewrite of HTML templates — add CSS, layout structure, navigation, settings page template.
- **`internal/sftpadmin/server.go`**: New HTTP handlers for settings CRUD (GET/POST /settings).
- **`internal/config/shared.go`** (new): Shared `SharedConfig` struct with JSON read/write, used by both services.
- **`internal/server/server.go`** (file-sharing): Read config from `/etc/app/config.json` on each request instead of env vars.
- **`docker-compose.yml`**: Add named volume `app-config` mounted to both services at `/etc/app`.
- **Both Dockerfiles**: Ensure `/etc/app` directory exists and is writable.
- **`openspec/specs/sftp-admin/spec.md`**: Delta for UI presentation requirements (navigation, layout).
- New spec at `openspec/changes/.../specs/admin-settings/spec.md` for settings management behavior.
- **Order: implement AFTER `file-share-link-management`** (it restructures `internal/server/server.go` and `internal/config/config.go`; this change's per-request overlay builds on that structure). Do NOT implement in parallel — both touch `internal/server/server.go` and `docker-compose.yml`.
