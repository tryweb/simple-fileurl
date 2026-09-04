# simple-fileurl

Alpine-based web service that turns a mounted share directory into
`PUBLIC_URL/<directory-hash>/<file-hash>` download links. First release
covers sharing and downloading only; there is no built-in SFTP service,
login, or upload endpoint.

## Quick start

```bash
cp .env.example .env
# Edit .env: set HOST_SHARE_PATH to the host directory that contains
# the SHARE_PREFIX subtree (e.g. the SFTP data root).
docker compose pull
docker compose up -d
curl http://localhost:8080/healthz
```

Open `http://localhost:8080/` for the file listing with copyable links.

## Configuration

| Variable         | Used by | Meaning |
|------------------|---------|---------|
| `HOST_SHARE_PATH`| Compose | Host data directory containing the `SHARE_PREFIX` subtree. Required. Never hashed or exposed. |
| `SHARE_PREFIX`   | Service | SFTP-visible logical prefix (e.g. `files`). Required. Scopes scanning and starts every directory hash input. |
| `PUBLIC_URL`     | Service | External base URL for rendered links (e.g. `https://weurl.everplast.net`). Required. Trailing slashes are trimmed. |
| `HASH_TARGET`    | Service | `file` (hash contents, default) or `filename` (hash basename). |
| `HASH_ALGORITHM` | Service | `md5` (default, 32 hex chars) or `sha256` (64 hex chars). |
| `PORT`           | Compose | Host-side published port (default `8080`). The container always listens on `8080`. |

The container mount target `/opt/sharefiles` is a fixed internal contract;
only the host source is configurable. The share is mounted read-only and the
service runs as a non-root user.

## Path mapping

```text
Host:      ${HOST_SHARE_PATH}/files/mydir/cron.txt
Container: /opt/sharefiles/files/mydir/cron.txt
SFTP view: files/mydir/cron.txt   <- hash input, never an absolute path
```

Directory hash input is the logical directory (`files/mydir`), filename
hash input is the basename (`cron.txt`), and content hash input is the file
bytes. Changing the host path without changing the logical tree keeps every
URL stable.

Uploader self-computation (no server round-trip needed):

```bash
printf '%s' 'files/mydir' | md5sum   # directory hash
printf '%s' 'cron.txt' | md5sum      # HASH_TARGET=filename
md5sum cron.txt                      # HASH_TARGET=file (fully uploaded file)
# -> https://weurl.everplast.net/<dir-hash>/<file-hash>
```

## Security notes

- Share links are bearer links: anyone holding the full URL can download.
  MD5 is kept for short, compatible URLs, not as a secrecy mechanism; use
  `sha256` where stronger collision resistance matters.
- Symlinks are never followed, non-regular files are refused, and hash
  collisions resolve to HTTP 409 instead of serving an arbitrary file.
- TLS terminates at the reverse proxy in front of this service.

## Rollback

The service never writes to the share directory, so rollback is stopping the
service and returning to the previous image:

```bash
docker compose down
# optionally: docker compose up -d --build <previous-tag>
```

## Development and CI

Local development uses `docker-compose.dev.yml`, which builds the image from
source and serves the repo-local `testdata/` fixture — no SFTP host needed:

```bash
docker compose -f docker-compose.dev.yml up -d --build
PORT=18080 bash test/smoke.sh
docker compose -f docker-compose.dev.yml down
```

`.github/workflows/ci.yml` (modeled on ai-engkit's pipeline shape) runs on
push/PR: `unit` (gofmt, vet, tests) → `smoke` (image build, compose up with
`testdata`, `test/smoke.sh`, logs on failure, always cleanup) → `push` to
GHCR on `v*` tags as `ghcr.io/tryweb/simple-fileurl:<tag>` plus a `sha-`
tag. Markdown/docs-only changes skip the pipeline.

## Endpoints

- `GET /` — HTML listing with share links.
- `GET /<directory-hash>/<file-hash>` — file download (404 unknown,
  409 collision, 400 malformed).
- `GET /healthz` — container health check (`{"status":"ok"}`).
