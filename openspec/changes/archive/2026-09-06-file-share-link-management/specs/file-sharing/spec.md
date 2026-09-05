## ADDED Requirements

### Requirement: Share link management API

The service SHALL expose a link management API under `/api/links` authenticated with a Bearer `ADMIN_TOKEN` (read from configuration; missing token fails fast at startup). It SHALL support creating, listing, viewing, updating, and deleting share links. Each link SHALL have a randomly generated 16-character URL-safe ID, a scope (`admin` for all namespaces or `user:<username>` for `files/` plus that user's directory), an optional bcrypt-hashed password, an optional expiry timestamp, a creation timestamp, and an optional description. The API SHALL never return password hashes — list and detail responses SHALL only indicate whether a password is set.

#### Scenario: Admin creates a scoped link
- **WHEN** an authenticated admin POSTs a link with scope `user:jonathan` and an optional password
- **THEN** the service returns 201 with the link ID and its public URL `/l/:linkId`

#### Scenario: Unauthenticated management request is rejected
- **WHEN** a client calls `/api/links` without a valid Bearer token
- **THEN** the service returns 401 and no link data

#### Scenario: Admin deletes a link
- **WHEN** an authenticated admin DELETEs an existing link ID
- **THEN** the service returns 204 and subsequent accesses to `/l/:linkId` return 404

#### Scenario: Link password can be rotated
- **WHEN** an authenticated admin PATCHes a link with a new password
- **THEN** the old password stops working and the new password grants access

### Requirement: Scoped link access pages

The service SHALL serve each link's file listing at `GET /l/:linkId`. Links without a password SHALL render the file list directly. Links with a password SHALL return a password form; submitting the correct password to `POST /l/:linkId/auth` SHALL set an HMAC-signed session cookie (24h) and redirect to the listing, while a wrong password SHALL return 401. The listing SHALL be filtered by the link's scope: `admin` scope shows all eligible namespaces, `user:<username>` scope shows `files/` plus `<username>/` only. Each file entry SHALL show its logical path, size, and a download URL using the existing `/<dirHash>/<fileHash>` route. Unknown or expired link IDs SHALL return 404.

#### Scenario: Passwordless link renders directly
- **WHEN** a client opens `/l/:linkId` for a link without a password
- **THEN** the service returns the filtered file listing

#### Scenario: Password gate grants scoped access
- **WHEN** a client submits the correct password for a `user:jonathan` link
- **THEN** the service sets a session cookie and shows `files/` plus `jonathan/` files, excluding other users' directories

#### Scenario: Expired link is gone
- **WHEN** a client opens `/l/:linkId` after the link's `expires_at`
- **THEN** the service returns 404

#### Scenario: Scope isolation holds
- **WHEN** a client with a `user:jonathan` session requests the JSON file list
- **THEN** no file with a logical path outside `files/` or `jonathan/` is included

### Requirement: Share link persistence

Links SHALL be persisted as JSON in `links.json` under the configured links directory (default `/var/lib/file-links`, on a named volume). Writes SHALL use atomic replacement (write-to-temp + rename) so readers never see a partial document. Expiry SHALL be enforced on access; no background cleanup job is required in v1 — expired entries remain on disk but are unreachable, and SHALL be purged when the next write occurs.

#### Scenario: Links survive container restart
- **WHEN** the file-sharing container restarts with the same `file-links` volume
- **THEN** all previously created links remain accessible with unchanged IDs and settings

#### Scenario: Concurrent link writes are safe
- **WHEN** two admins create links simultaneously
- **THEN** both links are stored without data loss

## REMOVED Requirements

### Requirement: Link generation interface
**Reason**: Replaced by the scoped share-link system. A single secret path cannot express per-audience visibility (admin vs per-user), and per-link passwords plus expiry replace the one global password gate.
**Migration**: Remove `ADMIN_PATH` / `ADMIN_PASSWORD` from configuration. Operators create scoped links via `POST /api/links` instead of sharing the `GET /<ADMIN_PATH>` URL. The download route `/<dirHash>/<fileHash>` is unchanged.

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
