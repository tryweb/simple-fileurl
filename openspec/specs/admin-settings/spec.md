# admin-settings Specification

## Purpose

Provides a settings management page in the SFTP admin UI so operators can view and modify the shared configuration without SSH access. All changes take effect immediately — no container restarts required.

## Requirements

### Requirement: Settings page displays all editable configuration fields

The admin UI SHALL serve a Settings page accessible from the main navigation. The page SHALL display all editable configuration fields grouped by the service they affect (`file-sharing` vs `sftp-admin`), showing the current value, a description, and the allowed values (where applicable).

#### Scenario: Settings page loads with current values
- **WHEN** an authenticated admin navigates to the Settings page
- **THEN** the page displays all editable config fields with their current values, grouped by target service

#### Scenario: Settings page is accessible from navigation
- **WHEN** an authenticated admin views any admin page
- **THEN** a navigation link to Settings is visible and reachable without typing a URL

### Requirement: Editable settings with validation

The admin UI SHALL allow editing the following configuration fields through form inputs with server-side validation:

| Field | Service | Allowed Values |
|---|---|---|
| `HashAlgorithm` | file-sharing | `md5`, `sha256` |
| `HashTarget` | file-sharing | `file`, `filename` |
| `PublicURL` | file-sharing | Non-empty URL |
| `AdminToken` | file-sharing | Non-empty secret (rotation invalidates API tokens and link sessions) |
| `SftpAdminPassword` | sftp-admin | Non-empty string |

The server SHALL validate each value against the same rules used at service startup. Invalid values SHALL be rejected with a field-level error message and the previous value preserved.

#### Scenario: Valid setting is saved
- **WHEN** an admin changes `HashAlgorithm` from `md5` to `sha256` and submits
- **THEN** the new value is persisted to `config.json` and displayed on the page

#### Scenario: Invalid setting is rejected
- **WHEN** an admin sets `PublicURL` to an empty string and submits
- **THEN** the request is rejected with an error message and the previous value is shown

### Requirement: Live config updates — no restart required

All settings changes SHALL take effect immediately without container restarts. The `file-sharing` service SHALL read the config file on each request. The `sftp-admin` service SHALL read `SftpAdminPassword` from the config file on each login attempt.

#### Scenario: Hash algorithm change takes effect immediately
- **WHEN** an admin changes `HashAlgorithm` from `md5` to `sha256`
- **THEN** subsequent file share URLs use SHA-256 hashes without any container restart

#### Scenario: Admin password change takes effect on next login
- **WHEN** an admin changes `SftpAdminPassword`
- **THEN** the next login attempt uses the new password

#### Scenario: Token rotation invalidates old credentials
- **WHEN** an admin changes `AdminToken`
- **THEN** the old Bearer token is rejected on the next management API call and existing link session cookies stop granting access

### Requirement: Settings persistence via shared config volume

Settings changes SHALL be persisted by writing to a shared `config.json` file on a named volume (`app-config`) mounted by both services at `/etc/app`. Writes SHALL use atomic replacement (write-to-temp + rename) to prevent corruption. The file format SHALL be JSON.

#### Scenario: Settings survive container restart
- **WHEN** an admin changes `PublicURL` and either container is restarted
- **THEN** the new `PublicURL` value is active after restart

#### Scenario: Concurrent writes are safe
- **WHEN** two admins submit settings changes simultaneously
- **THEN** both changes are applied without data loss (atomic writes)

### Requirement: Settings changes are auditable

Each settings change SHALL be logged with the timestamp, the admin session identifier, the field name, the old value (redacted for password fields), and the new value (redacted for password fields). The log SHALL be written to the admin service's stdout in structured format.

#### Scenario: Settings change is logged
- **WHEN** an admin changes `HashAlgorithm` from `md5` to `sha256`
- **THEN** the admin service logs: timestamp, session ID, field name, old value `***`, new value `***`
