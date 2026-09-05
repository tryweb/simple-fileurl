## Context

See proposal.md for motivation. The file-sharing service currently gates its whole listing behind one `ADMIN_PATH + ADMIN_PASSWORD` pair and cannot express per-audience visibility. This design replaces that gate with scoped share links, reusing the existing eligible-namespace model (`IsEligibleNamespace`: shared prefix + valid SFTP usernames) and the existing download route. `sftp-admin` is untouched — it never sees file data.

## Goals / Non-Goals

**Goals:**
- Scoped listings: `admin` links see all namespaces, `user:<name>` links see `files/` + `<name>/`
- Management API with token auth; per-link optional password + expiry
- JSON file persistence with atomic writes; no new infrastructure (no DB, no background jobs)
- Clean removal of the old listing (breaking, with migration path)

**Non-Goals:**
- sftp-admin UI integration (Phase 3 — follow-up change)
- Per-user quota, cross-user sharing, directory cleanup on user deletion
- Background expiry janitor (expiry enforced on access; lazy purge on write)

## Decisions

### D1: Stay in file-sharing (no move to sftp-admin)

**Decision:** Implement links in `file-sharing`, per the source doc's scheme 2.

**Rationale:** Keeps the security boundary (sftp-admin never mounts share data), keeps listing availability decoupled from user management, and lets file-sharing scale independently.

### D2: Storage — `links.json` on a named volume, no DB

**Decision:** New `internal/links` package with `LinkStore`: whole-map JSON at `<LinksDir>/links.json`, loaded at startup, mutated under a mutex, persisted with write-to-temp + rename.

**Rationale:** Link count is small (tens, not millions). A JSON file matches the existing manifest pattern (`sftp-users/users.json`) and needs no new infrastructure. Atomic writes give the same crash safety as the user manifest.

### D3: Passwords — bcrypt; sessions — HMAC cookie keyed by ADMIN_TOKEN

**Decision:** Link passwords stored as bcrypt hashes (cost 12). Successful password auth sets an HMAC-SHA256-signed cookie (`link_id + expiry`, 24h) using `ADMIN_TOKEN` as the signing key — no additional secret to configure or rotate.

**Rationale:** bcrypt is the standard for stored passwords; cost 12 is ~250ms per check, acceptable for an interactive auth flow. Reusing `ADMIN_TOKEN` as the HMAC key avoids a second secret while keeping sessions unforgeable without it.

**New dependency:** `golang.org/x/crypto` (bcrypt). `go.mod` currently has zero external deps — this is the first one.

### D4: Scope filtering reuses the namespace model

**Decision:** `user:<name>` scope = files whose top-level directory is `files/` (the configured shared prefix's first segment) or exactly `<name>`; `admin` scope = all eligible namespaces. Validation: `user:<name>` requires `name` to match the SFTP username pattern; unknown users are rejected at link creation.

**Rationale:** No new namespace logic — the eligibility rules from the per-user-dirs change are reused verbatim.

### D5: Expiry enforced on access, no janitor

**Decision:** Every access path checks `expires_at` before serving. No background goroutine. Optionally purge expired entries lazily during the next write.

**Rationale:** A scheduler adds lifecycle complexity (shutdown, tickers, tests) for zero user-visible benefit — expired links are already unreachable.

### D6: Independent ADMIN_TOKEN, fail fast when missing

**Decision:** The management API uses its own `ADMIN_TOKEN` env var (required, fail fast), not `SFTP_ADMIN_PASSWORD`.

**Rationale:** Separate service boundary, separate secret. The sftp-admin password protects user management; the file-sharing token protects link management. Sharing one secret couples the two services' rotation and blast radius. `AdminToken` is also runtime-rotatable through the shared config (`improve-admin-ui-and-settings`); rotation invalidates outstanding Bearer tokens and HMAC link sessions, which is the intended effect.

## Risks / Trade-offs

- **[BREAKING removal]** Old `ADMIN_PATH` URLs stop working on deploy. → Mitigation: migration step in tasks (create replacement links before removing config); document in release notes.
- **[First external dep]** `golang.org/x/crypto` is the project's first non-stdlib dependency. → Mitigation: it is the canonical Go crypto extension, version-pinned in `go.mod`; vendoring not required.
- **[bcrypt cost on slow hosts]** Cost 12 ≈ 250ms per password check on modest hardware. → Acceptable for interactive auth; link creation is rare. Lower to 10 only if measured pain.
- **[Cookie theft within 24h]** A stolen session cookie grants listing access until expiry. → Mitigation: HttpOnly + SameSite + Secure (when behind TLS); sessions are listing-only, never management.
- **[links.json growth]** Expired entries accumulate until a write purges them. → Negligible at expected scale; lazy purge bounds it.
- **[Volume ownership]** `file-sharing` runs as non-root `app`, so a fresh `file-links` volume (root-owned) rejects writes. → Mitigation: the image creates `/var/lib/file-links` owned by `app`; named-volume initialization preserves that ownership on first mount.
