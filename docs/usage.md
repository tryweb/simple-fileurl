# Usage Guide

## Web Downloads

The web service exposes:

| Method and path | Result |
|---|---|
| `GET /healthz` | `200` with `{"status":"ok"}` when the share is usable |
| `GET /<directory-hash>/<file-hash>` | File download, or `400`/`404`/`409` for invalid, missing, or ambiguous links |
| `GET /<ADMIN_PATH>` | Optional protected file listing |
| `GET /` | Empty `200` response when listing is gated or disabled |

Hash links are bearer links. Treat them as credentials and protect the public URL with TLS.

## Admin UI

Open `http://127.0.0.1:8081/` for a local Compose deployment and sign in with `SFTP_ADMIN_PASSWORD`.

The UI supports:

1. Create a user with a generated Ed25519 keypair.
2. Download the generated private key exactly once.
3. Register additional external public keys.
4. View public-key fingerprints.
5. Disable or re-enable users.

Generated private keys are held in Admin process memory only. Download and protect the key immediately:

```bash
chmod 600 alice_ed25519
```

If the one-time link is used twice, expires, or Admin restarts before download, create a replacement user/key and revoke the unusable one.

## SFTP Upload

After creating `alice` in Admin and waiting for reconciliation:

```bash
sftp -P 2222 -i ./alice_ed25519 alice@example-host
```

Inside the SFTP session, use paths below the visible `SHARE_PREFIX`:

```text
mkdir files/manual-test
put report.pdf files/manual-test/report.pdf
ls files/manual-test
quit
```

The upload is immediately available to the web service through its read-only mount. No container restart or file synchronization step is required.

## Compute A Link

For the default `HASH_ALGORITHM=md5` and `HASH_TARGET=file`:

```bash
DIR_HASH=$(printf '%s' 'files/manual-test' | md5sum | cut -d' ' -f1)
FILE_HASH=$(md5sum report.pdf | cut -d' ' -f1)
```

For `HASH_ALGORITHM=sha256`, replace `md5sum` with `sha256sum`. For `HASH_TARGET=filename`, hash `report.pdf` instead of its bytes.

Verify a download:

```bash
curl -fLo downloaded.pdf "https://files.example.com/$DIR_HASH/$FILE_HASH"
sha256sum report.pdf downloaded.pdf
```

## User Revocation

Disable the user in Admin. The SFTP reconciler removes the effective authorized-key file on its next poll. Existing sessions may finish, but new connections must fail. Re-enable the user to restore its authorized keys.

## Troubleshooting Checks

```bash
```

Common causes:

- SFTP does not start: check root ownership and writability of the chroot path.
- Upload fails: check `${SHARE_PREFIX}` group ownership, setgid, and group write permission.
- Web returns `404`: recompute the hash using the logical SFTP path and the configured hash target/algorithm.
- Admin does not start: set `SFTP_ADMIN_PASSWORD`.
- Key login fails: check that the user is enabled, the key file has mode `0600` locally, and the manifest has reconciled.
