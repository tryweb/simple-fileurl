## Context

See proposal.md for motivation. The current architecture uses a single `SHARE_PREFIX` directory (e.g. `files/`) as the sole namespace. All SFTP users share write access via group permissions. The web service scans only that one directory. This design adds per-user directories while preserving backward compatibility with the existing shared namespace and URL format.

## Goals / Non-Goals

**Goals:**
- Each SFTP user gets a personal directory (`/share/<username>/`) with write isolation from other SFTP users
- The existing `files/` shared directory continues to work unchanged
- Web service discovers and serves files from both shared and per-user namespaces
- URL format `/{dirHash}/{fileHash}` remains unchanged
- Admin listing behavior is unchanged (lists all discoverable files)

**Non-Goals:**
- Per-user quota management
- Admin UI changes to show per-user file listings or manage per-user directories
- Cross-user file sharing or permission delegation
- Per-user directory cleanup on user deletion (directories persist after user removal)
- Changes to the SFTP chroot model (all users still chroot to `/share`)

## Decisions

### Decision 1: Namespace discovery — scan all eligible top-level directories

**Choice:** The web service scans all top-level directories under the container share root (`/opt/sharefiles/`) that match either the configured `SHARE_PREFIX` or a valid SFTP username pattern (`^[a-z_][a-z0-9_-]{0,31}$`).

**Rationale:** This avoids introducing a new configuration variable for namespace lists. The set of eligible directories is derived from the existing SFTP user manifest plus the shared prefix. Directories that don't match either criterion are ignored, preventing accidental exposure of unrelated data.

**Alternatives considered:**
- Explicit `NAMESPACES` env var listing directories: more configuration burden, drift risk between SFTP users and web namespaces
- Scan everything under container root: risks exposing unintended directories

### Decision 2: Per-user directory permissions — dedicated WEB_GID group

**Choice:** Per-user directories are created with mode `0750`, owner = `<username>`, group = a dedicated `WEB_GID` (new env var, default `2001`). The web container's non-root process runs with supplementary group `WEB_GID`. SFTP users belong only to `SFTP_GID` (`sftpusers`), so they cannot access other users' directories.

**Rationale:** This provides both write isolation (only owner can write) and read isolation from other SFTP users (they lack the WEB_GID group). The web service can read files for serving without requiring root or per-user group membership.

**Alternatives considered:**
- `0755` (other-readable): simplest but allows all SFTP users to read other users' files
- ACLs: more flexible but adds complexity and Alpine `acl` package dependency
- Run web as root: violates existing spec requirement for non-root process
- Shared `SFTP_GID` for per-user dirs: would let all SFTP users read other users' files

### Decision 3: Per-user directory lifecycle — create on reconcile, never auto-delete

**Choice:** The reconcile script creates `/share/<username>/` when a user is first enabled. When a user is disabled or removed from the manifest, the directory and its contents remain on disk. Re-enabling the user restores access without data loss.

**Rationale:** Auto-deleting user data on disable is destructive and surprising. The directory is owned by the user, so even if the user account is removed from the SFTP container, the files persist on the host bind mount. Cleanup is a manual admin operation.

### Decision 4: Logical path construction — top-level dir becomes prefix

**Choice:** For files in per-user directories, the logical path is `<username>/<relative-path>`. For example, a file at `/share/jonathan/docs/notes.pdf` has logical path `jonathan/docs/notes.pdf` and directory hash is computed from `jonathan/docs`. The `SHARE_PREFIX` config value is NOT prepended to per-user paths.

**Rationale:** This keeps the logical path intuitive and matches the SFTP-visible path. The hash naturally differs from shared-prefix paths, so URL uniqueness is maintained without any URL format changes.

### Decision 5: Shared prefix directory — unchanged behavior

**Choice:** The `SHARE_PREFIX` directory (e.g. `files/`) retains its existing group-writable permissions (`SFTP_GID`, setgid, `umask 0002`). All SFTP users can read and write. The web service treats it identically to today.

**Rationale:** Backward compatibility. Existing deployments and share links must continue to work without changes.

## Risks / Trade-offs

- **[Risk] WEB_GID collision with host GID** → Mitigation: document that `WEB_GID` must not conflict with host GIDs. Default `2001` is unlikely to collide; make it configurable.
- **[Risk] Stale per-user directories accumulate** → Mitigation: document that directory cleanup is manual. Future admin UI could add a cleanup action (out of scope for this change).
- **[Risk] Web service scans directories that are not yet created** → Mitigation: the scan uses `WalkDir` which silently skips missing directories. A user directory created after the last scan appears on the next scan cycle.
- **[Trade-off] Per-user files require WEB_GID coordination** → The web container must run with the correct supplementary group. This adds a deployment constraint but provides proper isolation.
- **[Trade-off] No per-user listing in admin UI** → The admin listing shows all files from all namespaces mixed together. Users cannot easily see which files belong to which namespace. This is acceptable for now; the admin UI enhancement is deferred.
