# Development And Testing

## Development Environment

The dev Compose file creates a `share-data` named volume. `share-init` copies `testdata/` into it, sets the chroot/prefix permissions, and then both web and SFTP services use that volume.

```bash
```

Dev endpoints:

- Web: `http://localhost:18080`
- Admin: `http://<host>:18081` (remote-accessible on `0.0.0.0` by default; protect with TLS/private network/firewall)
- SFTP: `localhost:12222`
- Default dev Admin password: `dev-admin-password`

Stop and remove only dev data with:

```bash
```

## Test Layers

### Go Unit And Integration Tests

The repository uses a containerized Go toolchain so tests do not require Go on the host:

```bash
```

This image runs `gofmt`, `go vet`, and:

```bash
```

### SFTP Contract Tests

Run the shell checks directly:

```bash
sh test/sftp-config.sh
sh test/sftp-reconcile.sh
```

The tests cover SSH hardening, manifest validation, key-file permissions, invalid-manifest preservation, disabled users, malformed keys, and exact stale-user matching such as `bob` versus `bobby`.

The standalone test image is also independently buildable and runnable:

```bash
  sh -c 'cd /src && sh test/sftp-config.sh && sh test/sftp-reconcile.sh'
```

### Existing Web Smoke Test

```bash
PORT=18080 sh test/smoke.sh
```

Run it after starting dev Compose. It checks the existing file-sharing HTTP behavior; live SFTP-to-web testing is described below.

### Installer And Upgrade Script Checks

```bash
bash test/install-upgrade.sh
```

This validates shell syntax and the key install/upgrade safety steps without
starting or modifying a deployment.

## Manual End-To-End Test

1. Start dev Compose with `docker-compose.dev.yml`.
2. Log in to Admin and create a generated-key user.
3. Download the private key once and set mode `0600`.
4. Connect with `sftp -P 12222 -i <key> <user>@localhost`.
5. Upload a file below `files/` without restarting containers.
6. Compute the logical directory and content hashes.
7. Download the hash URL from port `18080` and compare bytes.
8. Disable the user in Admin and confirm a new SFTP login fails.
9. Confirm another enabled user still logs in.
10. Attempt password auth, PTY, forwarding, and paths above the chroot; all must be rejected or confined.

## Compose And Spec Validation

```bash
    SHARE_PREFIX=files \
    PUBLIC_URL=https://example.test \
    ADMIN_TOKEN=dummy \
    SFTP_ADMIN_PASSWORD=dummy \
    docker compose -f docker-compose.yml config

docker compose -f docker-compose.dev.yml config
openspec validate add-sftp-service --strict
openspec validate --specs
```

## CI Pipeline

`.github/workflows/ci.yml` runs:

1. Unit checks: format, vet, and race/shuffle tests.
2. Smoke job: builds web, SFTP, Admin, and SFTP test images.
3. Contract checks: runs SFTP tests on the runner and inside the test image.
4. Compose validation and dev web smoke test.
5. Tag push: publishes the three deployable images to GHCR.

Keep tests deterministic and avoid relying on a host Go installation. When changing a Dockerfile or shell contract, run both the host and in-image contract tests.
