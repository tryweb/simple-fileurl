# SFTP CI Verification Pattern

## Context

The project has three deployable containers: the existing web image, the SFTP image, and the SFTP Admin image. The host environment may not have a Go toolchain.

## Problem

Testing only the web `Dockerfile` and running `go test ./...` does not verify the SFTP Dockerfiles, shell contracts, image packaging, or the stronger race-test configuration. Production Compose also requires `SFTP_ADMIN_PASSWORD` during interpolation.

## Solution

CI performs these checks:

- `go test -race -shuffle=on -count=1 ./...` after `gofmt` and `go vet`.
- Builds `Dockerfile.sftp`, `Dockerfile.sftp-admin`, and the standalone `Dockerfile.sftp-test`.
- Runs `test/sftp-config.sh` and `test/sftp-reconcile.sh` on the runner and inside `simple-fileurl-sftp-test:ci`.
- Validates production Compose with `HOST_SHARE_PATH`, `SHARE_PREFIX`, `PUBLIC_URL`, and `SFTP_ADMIN_PASSWORD` set.
- Builds and publishes `simple-fileurl`, `simple-fileurl-sftp`, and `simple-fileurl-sftp-admin` under matching GHCR names on version tags.

## Why It Works

The Go verification image supplies a reproducible toolchain when Go is absent on the host. The standalone SFTP test image validates that the shell tests and their copied fixtures are packaged correctly, while CI image builds catch broken Dockerfile bases and COPY paths before release.

## Side Effects / Tradeoffs

- CI builds more images and takes longer than the original web-only pipeline.
- Compose validation intentionally fails when required production variables are missing; the CI command supplies safe test values rather than weakening the production contract.
- SFTP end-to-end network tests remain a separate live/dev smoke concern from the hermetic shell contract tests.

## Evidence

- `docker build -f Dockerfile.go-test` passed `gofmt`, `go vet`, and the full race suite.
- `docker build -f Dockerfile.sftp-test` passed, and both contract scripts passed inside the image.
- Production and dev `docker compose config` passed with required variables.
- `openspec validate add-sftp-service --strict` passed before archival.

## Related Files

- `.github/workflows/ci.yml`
- `Dockerfile.go-test`
- `Dockerfile.sftp-test`
- `test/sftp-config.sh`
- `test/sftp-reconcile.sh`
- `docker-compose.yml`

## Tags

`ci`, `docker`, `go`, `sftp`, `testing`, `github-actions`
