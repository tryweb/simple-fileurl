---
name: release
description: Run unit tests and smoke tests, auto-calculate semver, generate release notes, tag and push. Use when the user wants to cut a release.
---

# Release Skill

This skill automates the release process for simple-fileurl: dev-service
validation, unit plus smoke tests, version bump calculation, release note
generation, tagging, and pushing (which triggers the GHCR image publish).

## Workflow

When the user asks to release, follow these steps in order:

### 1. Ensure the Dev Service Is Running

The smoke test requires the `simple-fileurl-dev` service from
`docker-compose.dev.yml` to be running. Check if it is up:

```bash
docker inspect simple-fileurl-dev --format='{{.State.Status}}' 2>/dev/null
```

If the container is not running or does not exist, build and start it:

```bash
echo "[release] Dev service not running. Building and starting..."
docker compose -f docker-compose.dev.yml up --build -d
```

Then wait until it is healthy (max ~60s):

```bash
echo "[release] Waiting for dev service to be healthy..."
for i in $(seq 1 30); do
  STATUS=$(docker inspect simple-fileurl-dev --format='{{.State.Health.Status}}' 2>/dev/null)
  if [ "${STATUS}" = "healthy" ]; then
    echo "[release] Dev service is healthy."
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "[release] ERROR: dev service never became healthy."
    docker compose -f docker-compose.dev.yml logs --tail=20
    exit 1
  fi
  sleep 2
done
```

Note: in sandboxes where the Docker daemon cannot see the host filesystem
(Docker-out-of-Docker), the `./testdata` bind mount resolves empty and the
service fails with `stat /opt/sharefiles/files: no such file`. That is an
environment limitation, not a code bug — run the release from a machine
where the daemon shares the working tree, or substitute an equivalent
named volume for verification only.

### 2. Run Tests

Two gates, both must pass. Stop and report on the first failure —
do not proceed with the release.

```bash
# === 1. Unit tests (fmt, vet, all packages) ===
echo "[release] Running unit tests..."
export PATH="$HOME/sdk/go/bin:$PATH"
test -z "$(gofmt -l .)"
go vet ./...
go test ./... -count=1
```

```bash
# === 2. Smoke test against the running dev service ===
echo "[release] Running smoke test..."
PORT=18080 bash test/smoke.sh
```

If the service is reachable at a non-localhost base (e.g. a bridge
gateway), pass it explicitly:

```bash
BASE=http://<reachable-host>:18080 bash test/smoke.sh
```

### 3. Check for Uncommitted Changes

```bash
git status --short
```

**If there are uncommitted changes:**
1. **Ask the user** if they want to commit them before release
2. If the user confirms, commit with a conventional-commit message:
   - `feat:` for new features
   - `fix:` for bug fixes
   - `chore:` for maintenance, `docs:` for documentation, etc.
3. **Commit before continuing**

**If the working tree is clean:** proceed to version determination.

Do not release with uncommitted changes.

### 4. Determine Current and Next Version

```bash
git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"
```

Analyze `git log` since the last tag:

- **MAJOR**: any commit contains `BREAKING CHANGE` or `!:` in the subject
- **MINOR**: any commit starts with `feat:` or `feat(`
- **PATCH**: `fix:`, `docs:`, `style:`, `refactor:`, `perf:`, `test:`, `ci:`, `chore:`, or anything else

Calculate the next version (e.g. `v0.1.0`). If no previous tag exists,
use `v0.0.0` as the base and cut the first release as `v0.0.1`.

### 5. Generate Release Notes

```bash
LAST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
if [ -n "$LAST_TAG" ]; then
  git log ${LAST_TAG}..HEAD --oneline --no-merges
else
  git log --oneline --no-merges
fi
```

Categorize into sections for the user (strip the type prefix):

```
## Features
- descriptions from feat:

## Bug Fixes
- descriptions from fix:

## Other Changes
- descriptions from docs, refactor, chore, ci, test, etc.
```

### 6. Confirm with User

Present the calculated version and the release notes. Ask for confirmation
before tagging. Never push without explicit user confirmation.

### 7. Tag and Push

Upon confirmation:

```bash
git tag -a v{VERSION} -m "Release v{VERSION}"
git push origin main
git push origin v{VERSION}
```

Pushing a `v*` tag triggers the CI `push` job, which publishes
`ghcr.io/tryweb/simple-fileurl:{tag}` plus a `sha-` tag.

### 8. Report

After push, inform the user:
- New version tag
- GHCR image URL: `ghcr.io/tryweb/simple-fileurl:{version}`
- CI workflow run URL for the tag build

## Rules

- Never skip the test step (unit AND smoke)
- Never release with uncommitted changes (must commit first)
- Never push without user confirmation
- If `git log` is empty since the last tag, warn the user
- Use semver format: `v{MAJOR}.{MINOR}.{PATCH}`
