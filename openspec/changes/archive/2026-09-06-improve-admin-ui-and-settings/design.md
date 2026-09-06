## Context

The project has two containers: `file-sharing` (Go HTTP service serving hashed file URLs) and `sftp-admin` (Go HTTP service managing SFTP users via a web UI). Both currently read configuration from environment variables sourced from a shared `.env` file via `docker compose`. The `sftp-admin` UI is bare HTML with no CSS, no navigation, and only user management.

The `file-sharing` service reads config once at startup (`config.Load()` in `main.go`) and does not support runtime changes. To enable live config updates without container restarts, both services will read from a shared config file on a named volume.

## Goals / Non-Goals

**Goals:**
- Professional-looking admin UI with navigation, layout, and responsive design
- Settings page to view/edit configuration values from the UI
- Live config updates — no container restarts required
- Atomic config file writes to prevent corruption

**Non-Goals:**
- Multi-user admin roles or RBAC
- Changing the `.env` file format or location (it remains for initial container config)
- Hot-reload via file watcher (services re-read config on each request instead)

## Decisions

### D1: UI rendering — stay server-rendered, add embedded CSS

**Decision:** Keep Go `html/template` server-rendered pages. Add a shared CSS block embedded in a base layout template. No JavaScript framework, no build step.

**Rationale:** The existing codebase has zero frontend dependencies and no build pipeline. Adding React/Vue would introduce Node.js, bundling, and deployment complexity disproportionate to the UI scope. Embedded CSS (in a `{{define "styles"}}` block) avoids an extra HTTP request and keeps the single-binary deployment model.

**Alternatives considered:**
- External CSS file served statically: adds a route and file serving complexity for minimal gain
- htmx or Alpine.js for interactivity: unnecessary — settings forms are simple POST submissions with full-page reloads

### D2: Layout — top navigation bar

**Decision:** Single horizontal top nav bar with service name on the left and nav links (Users, Settings) + Sign out on the right. Content area below. No responsive breakpoint logic — the nav bar wraps naturally on narrow viewports.

**Rationale:** Two-page app. A top nav is ~20 lines of CSS vs ~80 for a collapsible sidebar. Add a sidebar when there are 3+ pages.

### D3: Shared config volume — both services read from `/etc/app/config.json`

**Decision:** Create a named volume `app-config` mounted at `/etc/app` in both containers. Store config as a JSON file (`config.json`). The `sftp-admin` service writes to it via the UI; the `file-sharing` service reads it on each request.

**Rationale:** A shared volume file eliminates the need for container restarts entirely. Both services see the same file. JSON is easier to parse and update atomically than `.env` (no comment preservation, no quoting edge cases). The file is small (<1KB) so per-request reads are negligible.

**Implementation detail:**
- `sftp-admin` writes: marshal config struct to JSON, write to temp file, rename atomically
- `file-sharing` reads: unmarshal JSON on each request. The file is <1KB; JSON unmarshal is ~10μs. No caching — add it only if profiling shows overhead.
- Initial config is seeded from `.env` at first boot if `config.json` doesn't exist

### D4: Config schema — typed struct with validation

**Decision:** Define a `SharedConfig` struct in `internal/config/shared.go` with fields matching the surviving runtime settings: `HashAlgorithm`, `HashTarget`, `PublicURL`, `AdminToken`, `SftpAdminPassword`. (`AdminPath` / `AdminPassword` are removed by `file-share-link-management` and are NOT part of the shared config; `AdminToken` is added by it — rotation invalidates outstanding API tokens and link sessions by design.) Each field has a `Validate() error` method. Both services import this package. The `sftp-admin` service validates before writing; the `file-sharing` service validates on read and falls back to defaults on error.

**Note:** `WebGID` is NOT included — it controls directory permissions and container `group_add`, which require filesystem changes and container restarts. It stays as a deployment-time env var, not a runtime-editable setting.

**Rationale:** Both services already import `internal/config`. One source of truth for the config schema and validation rules prevents drift.

### D5: Settings metadata — static registry for UI rendering

**Decision:** Define a static `SettingsRegistry` in `sftp-admin` that maps each config field to `{Key, Description, AllowedValues, IsSecret}`. The settings page renders this metadata alongside each input. No restart signaling needed — all changes take effect immediately.

**Rationale:** Keeps the UI rendering declarative. Adding a new setting only requires adding one row to the registry.

### D6: CSS approach — single embedded stylesheet with CSS custom properties

**Decision:** One `<style>` block in the base layout template using CSS custom properties for colors/spacing. Light color scheme with clear visual hierarchy. No dark mode in v1.

**Rationale:** Minimal token cost, no external dependencies, easy to tune. CSS custom properties make future theming trivial.

## Risks / Trade-offs

- **[Per-request config read]** The `file-sharing` service reads `config.json` on every request. → Mitigation: The file is <1KB and JSON unmarshal is ~10μs. Negligible overhead. Add caching only if profiling shows it matters.
- **[Config file corruption]** If `sftp-admin` crashes mid-write, `config.json` could be corrupted. → Mitigation: Atomic writes (write-to-temp + rename) prevent partial writes. The `file-sharing` service validates on read and falls back to defaults on error.
- **[Initial config seeding]** The first boot needs to populate `config.json` from `.env`. → Mitigation: Both services check if `config.json` exists at startup; if not, they create it from env vars (with validation).
- **[Password redaction in logs]** Old/new password values are logged as `***`. The actual values are only in `config.json`. → Consistent with security best practices.
- **[Volume permissions]** Only `sftp-admin` (root) writes `config.json`; `file-sharing` runs as non-root `app` and only reads. A root-created file defaults to 0600 and is unreadable to the reader. → Mitigation: `Save` chmods the file to 0644 after the atomic rename (secrets were already visible via container env inspection — no new exposure); both images create `/etc/app` at build time.
- **[First-boot race]** Both services seed `config.json` from env vars if missing. If both start simultaneously, both may try to write. → Mitigation: Only `sftp-admin` seeds the file. `file-sharing` reads only — if the file is missing, it uses env vars and logs a warning.
