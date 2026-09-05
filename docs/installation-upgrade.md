# Installation And Upgrade

## Prerequisites

- Docker Engine with Compose v2 and a reachable Docker daemon.
- `curl` or `wget` for the installer, plus `curl`, `sftp`, and `ssh` for checks.
- A host data directory containing the `${SHARE_PREFIX}` subtree.
- Permission to create/chown the host share as `root:root` and set its prefix group to `SFTP_GID`.
- TLS termination or a private network before exposing Admin beyond localhost.

## Fast Install

The installer follows the same two-script pattern as ai-engkit: a first run
downloads the Compose file and environment template, prompts for required
values, prepares the share, pulls images, and starts services. If a Compose
file already exists, `install.sh` delegates to `upgrade.sh`.

From a new directory:

```bash
mkdir -p /opt/simple-fileurl
cd /opt/simple-fileurl
curl -fsSL https://raw.githubusercontent.com/tryweb/simple-fileurl/vX.Y.Z/install.sh | RELEASE_REF=vX.Y.Z bash
```

The installer prompts for:

- `HOST_SHARE_PATH`
- `SHARE_PREFIX` (default `files`)
- `PUBLIC_URL`
- `ADMIN_TOKEN` (secret Bearer token for the share-link management API)
- `IMAGE_TAG` (an immutable `vX.Y.Z` or `sha-<hex>` release tag)
- `SFTP_ADMIN_PASSWORD`

For a checked-out repository:

```bash
./install.sh
```

For automation, create `.env` with all required values before invoking the
script; a fully preseeded file needs no TTY. If a required value is missing,
the installer prompts through `/dev/tty`.

## Host Share Requirements

For the default `SHARE_PREFIX=files` and `SFTP_GID=2000`:

```bash
sudo mkdir -p /srv/simple-fileurl/files
sudo chown root:root /srv/simple-fileurl
sudo chmod 0755 /srv/simple-fileurl
sudo chown root:2000 /srv/simple-fileurl/files
sudo chmod 2775 /srv/simple-fileurl/files
```

The installer performs the equivalent preparation using root or `sudo`.
Every directory component used as the chroot path must be owned by root and
not be group/other writable. Existing files under `files` must be readable by
the web container.

## Configuration

The installer creates `.env` from `.env.example`. Set or review:

```dotenv
HOST_SHARE_PATH=/srv/simple-fileurl
SHARE_PREFIX=files
PUBLIC_URL=https://files.example.com
ADMIN_TOKEN=<strong-secret>
SFTP_ADMIN_PASSWORD=<strong-secret>
SFTP_GID=2000
IMAGE_TAG=v0.2.0
```

The production Compose file pulls three images for the same immutable release:

- `ghcr.io/tryweb/simple-fileurl:${IMAGE_TAG}`
- `ghcr.io/tryweb/simple-fileurl-sftp:${IMAGE_TAG}`
- `ghcr.io/tryweb/simple-fileurl-sftp-admin:${IMAGE_TAG}`

Missing `HOST_SHARE_PATH`, `SHARE_PREFIX`, `PUBLIC_URL`, `ADMIN_TOKEN`,
`IMAGE_TAG`, or `SFTP_ADMIN_PASSWORD` causes Compose interpolation to fail
instead of starting an incomplete deployment.

The installer and upgrader download `artifact-checksums.txt` from the selected
release ref and verify the Compose, environment template, and upgrade script
before using them. Set `RELEASE_REF` or `REPO_URL` when using an internal
release mirror; keep the checksum manifest alongside those artifacts.

## Manual Install Without The Script

```bash
cp .env.example .env
# Edit .env, especially HOST_SHARE_PATH, PUBLIC_URL, ADMIN_TOKEN, IMAGE_TAG,
# and SFTP_ADMIN_PASSWORD.
docker compose config
docker compose pull
docker compose up -d
docker compose ps
curl -f "$(grep '^PUBLIC_URL=' .env | cut -d= -f2-)/healthz"
```

## Seed And First User

The optional `SFTP_SEED_USER` and `SFTP_SEED_PUBKEY` values are consumed only
by `sftp-admin` and only when `sftp-users/users.json` does not exist. The SFTP
container mounts the manifest read-only and never seeds it.

Alternatively, leave the seed variables empty and create the first user
through the Admin UI after installation.

## Upgrade

`upgrade.sh` creates a timestamped backup of `docker-compose.yml` and `.env`,
downloads the selected release's Compose definition, appends missing environment keys
without overwriting existing values, pulls all three images, recreates the
services, and waits for the web health endpoint.

```bash
cd /opt/simple-fileurl
./upgrade.sh                 # use IMAGE_TAG already in .env
./upgrade.sh v0.2.0          # set IMAGE_TAG and upgrade all services
```

Remote invocation is also supported:

```bash
curl -fsSL https://raw.githubusercontent.com/tryweb/simple-fileurl/vX.Y.Z/upgrade.sh | RELEASE_REF=vX.Y.Z bash
```

Set `REPO_URL` or `RELEASE_REF` to use an internal/versioned release mirror. Set `BACKUP_RETENTION` to control
how many `backups/upgrade_*` directories are retained. The host share and
`sftp-users` volume are retained across image replacement.

After an upgrade, verify:

```bash
curl -f "$(grep '^PUBLIC_URL=' .env | cut -d= -f2-)/healthz"
```

Then test Admin login, an existing SFTP key, and a hash download.

### Migrating From The Secret-Path Listing

Releases with share links remove `ADMIN_PATH` / `ADMIN_PASSWORD`: old
`/<ADMIN_PATH>` URLs stop working on deploy. Before upgrading:

1. Create replacement scoped links via `POST /api/links` (see docs/usage.md)
   for every audience that used the old listing URL.
2. Add the new required `ADMIN_TOKEN` variable to `.env` (the upgrader appends
   missing keys; set a strong secret before starting the services).
3. After upgrading, open each replacement `/l/:linkId` URL to confirm the
   expected files are visible, then retire the old listing URL.

There is no automated migration: links are recreated through the new API.

The `promote.yml` workflow currently promotes the main web image to `latest`.
For a coordinated SFTP release, use matching explicit `IMAGE_TAG` values for
all three images rather than assuming `latest` was promoted for every image.

## Backup And Restore

Back up the host share, the `sftp-users` volume, and the `file-links` volume
(share links). The `sftp-users` volume contains the manifest and public keys,
but never generated private keys.

```bash
mkdir -p backups
docker run --rm \
  -v simple-fileurl_sftp-users:/data:ro \
  -v "$PWD/backups:/backup" \
  alpine:3.23 tar czf /backup/sftp-users.tar.gz -C /data .
docker run --rm \
  -v simple-fileurl_file-links:/data:ro \
  -v "$PWD/backups:/backup" \
  alpine:3.23 tar czf /backup/file-links.tar.gz -C /data .
```

Restore only while Admin, SFTP, and file-sharing are stopped:

```bash
docker compose stop sftp sftp-admin file-sharing
docker run --rm \
  -v simple-fileurl_sftp-users:/data \
  -v "$PWD/backups:/backup" \
  alpine:3.23 sh -c 'rm -rf /data/* /data/.[!.]* && tar xzf /backup/sftp-users.tar.gz -C /data'
docker run --rm \
  -v simple-fileurl_file-links:/data \
  -v "$PWD/backups:/backup" \
  alpine:3.23 sh -c 'rm -rf /data/* /data/.[!.]* && tar xzf /backup/file-links.tar.gz -C /data'
```

## Rollback

The simplest rollback is to select the previous immutable image tag:

```bash
./upgrade.sh v0.1.0
```

To restore only configuration from the newest backup, inspect the backup first:

```bash
ls -dt backups/upgrade_* | head -n1
cp backups/upgrade_<timestamp>/docker-compose.yml .
cp backups/upgrade_<timestamp>/.env .
docker compose up -d
```

To disable SFTP without removing data:

```bash
docker compose stop sftp sftp-admin
```

Do not use `docker compose down -v` in production; it removes named volumes.
