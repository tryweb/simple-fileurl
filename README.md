# simple-fileurl

Alpine-based web service that turns a mounted share directory into
`PUBLIC_URL/<directory-hash>/<file-hash>` download links. The deployment also
provides an isolated public-key-only SFTP upload service and localhost-bound
admin UI for managing SFTP users.

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
| `SFTP_PORT`      | Compose | Host-side SFTP port (default `2222`; container listens on `22`). |
| `SFTP_GID`       | SFTP | Numeric group ID for the writable `SHARE_PREFIX` subtree (default `2000`). |
| `SFTP_ADMIN_PORT`| Compose | Host-side admin UI port on `127.0.0.1` (default `8081`). |
| `SFTP_ADMIN_PASSWORD` | Admin | Required password for the SFTP admin UI. |
| `SFTP_SEED_USER` / `SFTP_SEED_PUBKEY` | SFTP/Admin | Optional first SFTP user/key; used only when `sftp-users/users.json` is absent. |

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

## SFTP upload

Production requires `${HOST_SHARE_PATH}` to be owned by `root:root` and not
group/other writable, because OpenSSH checks every `ChrootDirectory` component.
The `${HOST_SHARE_PATH}/${SHARE_PREFIX}` directory must use `SFTP_GID`, be
setgid and group-writable (normally `2775`). Existing files must be readable by
the web service; the SFTP service creates new files as `0664` and directories
as `0775`.

The SFTP service chroots users at `/share`, disables password, shell and
forwarding access, and exposes the logical `SHARE_PREFIX` tree. After an admin
creates a user, connect with the downloaded key:

```bash
sftp -P 2222 -i ./alice_ed25519 alice@example-host
put report.pdf files/mydir/report.pdf
```

The SFTP container reconciles new users from `sftp-users/users.json` without a
restart. The web service continues to mount the same directory read-only.

## SFTP admin UI

Open `http://127.0.0.1:8081/` after setting `SFTP_ADMIN_PASSWORD`. The UI can
create users with generated Ed25519 keys, register external public keys, list
fingerprints, and disable/enable users. A generated private key is delivered
once and is never stored or recoverable; save it immediately, or revoke the
user and generate a replacement.

Do not publish the admin port to the public internet. Use a TLS reverse proxy
or a private network for remote administration. Back up the `sftp-users`
Docker volume; it contains the user manifest and public keys, but never private
keys.

Overwriting a file through SFTP changes its `HASH_TARGET=file` content hash and
can invalidate an old share link. Use `HASH_TARGET=filename` when stable
filename-based identifiers are preferred.

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
- TLS terminates at the reverse proxy in front of this service and must also
  protect remote access to the admin UI.

## Rollback

The web service never writes to the share directory. To roll back SFTP, stop
the `sftp` and `sftp-admin` services while retaining the share data and
`sftp-users` volume; the web service can continue independently:

```bash
docker compose stop sftp sftp-admin
# optionally: docker compose up -d --build <previous-tag>
```

## Development and CI

Local development uses `docker-compose.dev.yml`, which copies the repo-local
`testdata/` fixture into a root-owned named volume for both web and SFTP — no
external SFTP host is needed:

```bash
docker compose -f docker-compose.dev.yml up -d --build
PORT=18080 bash test/smoke.sh
docker compose -f docker-compose.dev.yml down -v
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
