# SFTP Manifest Ownership And Mounts

## Context

The SFTP service and the `sftp-admin` service share the `sftp-users` volume. The manifest is the only control-plane data exchanged between them.

## Problem

Letting both containers seed or write `users.json` creates first-boot last-writer-wins behavior. Letting the network-facing SFTP container write the manifest also allows a compromised SFTP process to alter its own authorization state.

## Solution

- `sftp-admin` is the only manifest seeder and writer.
- `sftp` mounts `/var/lib/sftp-users` read-only.
- `sftp-admin` mounts the same volume read-write and uses atomic temporary-file plus rename updates.
- SFTP only reconciles the manifest into local accounts and authorized-key files.
- The SFTP container does not depend on `SFTP_ADMIN_PASSWORD` to start.

## Why It Works

The admin process owns all mutations, while the SFTP process can only consume complete manifest versions. A read-only mount prevents SFTP-side authorization changes even if its SSH-facing process is compromised.

## Side Effects / Tradeoffs

- A seed user appears when `sftp-admin` starts, then the SFTP polling loop reconciles it without an SFTP restart.
- Restarting `sftp-admin` before a pending generated-key download loses the in-memory private key by design; the user must be recreated.

## Evidence

- `openspec validate --specs` passed for `sftp-service` and `sftp-admin`.
- `test/sftp-config.sh` passed the single-seeder contract.
- Compose inspection showed `sftp-users:ro` for `sftp` and `rw` for `sftp-admin`.
- Live test created users through Admin, logged in through SFTP without restart, and revoked a user while another remained usable.

## Related Files

- `docker-compose.yml`
- `docker-compose.dev.yml`
- `cmd/sftp-admin/main.go`
- `internal/sftpadmin/store.go`
- `sftp/entrypoint.sh`
- `sftp/reconcile.sh`

## Tags

`sftp`, `manifest`, `authorization`, `docker-compose`, `least-privilege`
