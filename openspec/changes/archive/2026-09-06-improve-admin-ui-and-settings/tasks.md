## 1. Base Layout & CSS Foundation

- [x] 1.1 Create a base layout template (`baseTemplate`) in `internal/sftpadmin/ui.go` with shared `<head>`, CSS custom properties (colors, spacing, typography), top navigation bar (service name left, nav links + Sign out right), and a `{{block "content"}}` placeholder. Verify by rendering a test page that shows the top nav with "Users" and "Settings" links and a content area below.
- [x] 1.2 Write the embedded CSS stylesheet covering: reset/normalize, top nav bar layout, form styling (inputs, buttons, selects), table styling, card/section styling, alert/notice styling, and utility classes. Verify by checking the login page renders with proper layout in a browser.

## 2. Migrate Existing Pages to New Layout

- [x] 2.1 Refactor `loginTemplate` to extend the base layout. Replace the bare `<h1>` + `<form>` with a centered card layout. Verify the login page renders correctly with the new styles.
- [x] 2.2 Refactor `usersTemplate` to extend the base layout. Move user management into a card-based section. Improve the user table styling (zebra striping, proper spacing, action buttons inline). Verify the Users page renders with top nav and styled table.
- [x] 2.3 Refactor `createdTemplate` to extend the base layout. Style the one-time download link as a prominent call-to-action. Verify the user-created confirmation page renders correctly.

## 3. Shared Config Volume & JSON读写

- [x] 3.1 Create `internal/config/shared.go` with a `SharedConfig` struct holding the surviving runtime settings (`HashAlgorithm`, `HashTarget`, `PublicURL`, `AdminToken`, `SftpAdminPassword`; `AdminPath`/`AdminPassword` removed by `file-share-link-management`) plus `Load(path string)`, `Save(path string)`, and `Validate() error` methods. Use atomic writes (temp file + rename). Add unit tests. Verify tests pass.
- [x] 3.2 Update `file-sharing` service: keep startup config from env vars as the base. Add a `LoadSharedConfig(path string) (*SharedConfig, error)` call in the request handlers that need dynamic values (`server.go`). On each request, read `config.json` and overlay its values on top of the startup config. If `config.json` doesn't exist or is invalid, use startup config unchanged. Verify by setting a config value via the admin UI and seeing it take effect without restart.
- [x] 3.3 Update `sftp-admin` service (`cmd/sftp-admin/main.go`): on login, read `SftpAdminPassword` from `config.json` first, fall back to env var if the file doesn't exist, fall back to empty (fail fast) if neither is set. Priority: `config.json` > env var > default. Verify the service starts with config file, with env var only, and with neither (fails fast).

## 4. Settings Backend — JSON读写 + Validation

- [x] 4.1 Define the settings metadata table in `internal/sftpadmin/settings.go`: a `SettingDef` struct with `{Key, Description, AllowedValues, IsSecret}` and a static `SettingsRegistry` slice covering all 5 fields. Add `GetSettings(configPath string) ([]SettingValue, error)` that reads `config.json` and returns current values merged with metadata. Verify with unit tests.
- [x] 4.2 Add `ApplySetting(configPath string, key string, value string) error` that validates the value against the setting's rules, updates the `SharedConfig` struct, and saves atomically. Add unit tests. Verify tests pass.

## 5. Settings HTTP Handlers & UI

- [x] 5.1 Add `GET /settings` handler in `internal/sftpadmin/server.go` that reads `config.json`, merges with `SettingsRegistry`, and renders the settings page template. Require auth. Verify by curling the endpoint with a valid session cookie and checking the HTML output contains all settings with current values.
- [x] 5.2 Add `POST /settings` handler that accepts form submissions, validates each field, calls `ApplySetting`, and re-renders the page with success/error messages. Require auth + CSRF. Verify by POSTing valid and invalid values and checking responses.
- [x] 5.3 Create the settings page template in `ui.go` with: grouped sections (file-sharing / sftp-admin), appropriate input types (text, select dropdown for HASH_ALGORITHM/HASH_TARGET, password field for secrets), field descriptions, and allowed-value hints. No restart badges needed. Verify the page renders correctly.

## 6. Docker & Deployment Changes

- [x] 6.1 Update `docker-compose.yml`: add named volume `app-config` to the `volumes:` section, mount it to `file-sharing` and `sftp-admin` at `/etc/app:rw`. Preserve existing `WEB_GID`, `group_add`, and all other settings from the per-user-dirs change. Verify `docker compose config` parses without errors.
- [x] 6.2 Update both Dockerfiles to ensure `/etc/app` directory exists and is writable. Verify the images build successfully.
- [x] 6.3 Add first-boot config seeding in `sftp-admin` only: if `/etc/app/config.json` doesn't exist at startup, create it from env vars (validated; `AdminPath`/`AdminPassword` are NOT seeded — removed by `file-share-link-management`). `file-sharing` does NOT seed — if the file is missing, it uses env vars and logs a warning. Verify by deleting `config.json` and restarting — `sftp-admin` recreates it, `file-sharing` falls back to env vars.

## 7. Integration & Verification

- [x] 7.1 Run all existing unit tests (`go test ./...`) and verify no regressions. Add new tests for settings handlers and config read/write.
- [x] 7.2 Build both Docker images and run `docker compose up` locally. Verify: login page renders with new styles, Users page works as before, Settings page shows all 5 settings with correct current values, changing a setting takes effect immediately (no restart), and both services see the same config.
- [x] 7.3 Verify the layout at mobile viewport width (< 768px) — top nav wraps naturally, forms remain usable.
