# Installation And Upgrade

## Prerequisites

- Docker Engine with Compose v2.
- A host data directory containing the `${SHARE_PREFIX}` subtree.
- A host-side `SFTP_GID` group ID that can own the writable prefix.
- `curl`, `sftp`, and `ssh` for operational checks.
- TLS termination or a private network before exposing Admin beyond localhost.

## Prepare The Host Share

For the default `SHARE_PREFIX=files` and `SFTP_GID=2000`:

```bash
sudo mkdir -p /srv/simple-fileurl/files
sudo chown root:root /srv/simple-fileurl
sudo chmod 0755 /srv/simple-fileurl
sudo chown root:2000 /srv/simple-fileurl/files
sudo chmod 2775 /srv/simple-fileurl/files
```

Every directory component used as the chroot path must be owned by root and not be group/other writable. Existing files under `files` must be readable by the web container.

## Configure Production

```bash
cp .env.example .env
```

Set at least:

```dotenv
HOST_SHARE_PATH=/srv/simple-fileurl
SHARE_PREFIX=files
PUBLIC_URL=https://files.example.com
SFTP_ADMIN_PASSWORD=<strong-secret>
SFTP_GID=2000
```

Review `ADMIN_PATH`, `ADMIN_PASSWORD`, `HASH_TARGET`, `HASH_ALGORITHM`, `SFTP_PORT`, and `SFTP_ADMIN_PORT` before deployment. Do not use the example Admin password in production.

## Install

```bash
curl -f https://files.example.com/healthz
```

The production Compose file pulls three images from GHCR:

- `ghcr.io/tryweb/simple-fileurl:${IMAGE_TAG:-latest}`
- `ghcr.io/tryweb/simple-fileurl-sftp:${IMAGE_TAG:-latest}`
- `ghcr.io/tryweb/simple-fileurl-sftp-admin:${IMAGE_TAG:-latest}`

If `HOST_SHARE_PATH`, `SHARE_PREFIX`, `PUBLIC_URL`, or `SFTP_ADMIN_PASSWORD` is missing, Compose fails during interpolation instead of starting an incomplete deployment.

## First User And Seed

The optional `SFTP_SEED_USER` and `SFTP_SEED_PUBKEY` values are consumed only by `sftp-admin` and only when `sftp-users/users.json` does not exist. The SFTP container mounts the manifest read-only and never seeds it.

Alternatively, leave the seed variables empty and create the first user through the Admin UI after installation.

## Upgrade

Use an immutable version tag for controlled upgrades:

```bash
export IMAGE_TAG=v0.2.0
```

The host share and `sftp-users` volume are retained across image replacement. After an upgrade, verify `/healthz`, Admin login, an existing SFTP key, and a hash download.

The `promote.yml` workflow currently promotes the main web image to `latest`. For a coordinated SFTP release, use matching explicit `IMAGE_TAG` values for all three images rather than assuming `latest` was promoted for every image.

## Backup And Restore

Back up both the host share and the `sftp-users` volume. The volume contains the manifest and public keys, but never generated private keys.

```bash
docker run --rm \
  -v simple-fileurl_sftp-users:/data:ro \
  -v "$PWD/backups:/backup" \
  alpine:3.23 tar czf /backup/sftp-users.tar.gz -C /data .
```

Restore only while the Admin and SFTP services are stopped, then start them so SFTP reconciles the restored manifest:

```bash
  -v simple-fileurl_sftp-users:/data \
  -v "$PWD/backups:/backup" \
  alpine:3.23 sh -c 'rm -rf /data/* /data/.[!.]* && tar xzf /backup/sftp-users.tar.gz -C /data'
```

## Rollback

```bash
export IMAGE_TAG=v0.1.0
```

To disable SFTP without removing data:

```bash
```

Do not use `docker compose down -v` in production; it removes named volumes.
