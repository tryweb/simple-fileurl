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

### 1. Check Release Preconditions

Before building or starting anything:

```bash
BRANCH=$(GIT_MASTER=1 git branch --show-current)
test "$BRANCH" = main || { echo "[release] ERROR: release must run from main" >&2; exit 1; }

if [ -n "$(GIT_MASTER=1 git status --short)" ]; then
  echo "[release] Working tree is dirty. Stop, commit the changes, then restart the release." >&2
  exit 1
fi
```

The release must not test or build from uncommitted changes.

The host needs only Git, Bash, and Docker (plus Docker Compose). Go, curl,
and jq run inside ephemeral containers below — do not require them on
the host:

```bash
set -euo pipefail
command -v docker >/dev/null 2>&1 || { echo "[release] ERROR: Docker is required." >&2; exit 1; }
docker compose version >/dev/null
```

Pin the tested commit here — before step 2 builds anything. Every later
step re-checks that `HEAD` still equals this SHA. Each `bash` block below
runs in a fresh shell, so record this value and re-export it literally at
the top of every block that needs it (same for `RID` from step 2):

```bash
set -euo pipefail
TESTED_SHA="$(GIT_MASTER=1 git rev-parse HEAD)"
echo "[release] tested SHA: $TESTED_SHA"
```

### 2. Start an Isolated Dev Stack From the Current Checkout

Never touch the default dev project (`simple-fileurl-dev`,
`simple-fileurl-sftp-dev`, …). Every release run uses its own Compose
project plus `docker-compose.release.yml`, which removes hardcoded
`container_name` values, all published host ports, and dev image tags, and
pins the service env — so the release stack cannot collide with running dev
services even when the host environment is polluted. Write the full
`docker compose` command literally in every block (never a `$STRING`
command variable, array, or helper function holding the command, to keep
the commands compatible with this environment's shell gate). `RID` is
always double-quoted and only ever appears in argument position. Trap
`ERR` so a failed gate tears down this run's stack instead of leaking it:

```bash
set -euo pipefail
RID="release-$$"
TESTED_SHA="<sha-from-step-1>"
export RELEASE_RID="$RID"
trap 'docker compose -p "$RID" -f docker-compose.dev.yml -f docker-compose.release.yml down -v --remove-orphans >/dev/null 2>&1 || true; docker image rm "simple-fileurl-release:$RID" "simple-fileurl-release-sftp:$RID" "simple-fileurl-release-sftp-admin:$RID" "simple-fileurl-release-share:$RID" >/dev/null 2>&1 || true' ERR
echo "[release] project: $RID"
docker compose -p "$RID" -f docker-compose.dev.yml -f docker-compose.release.yml up --build --force-recreate -d
echo "[release] Waiting for isolated file-sharing to be healthy..."
for i in $(seq 1 30); do
  CID=$(docker compose -p "$RID" -f docker-compose.dev.yml -f docker-compose.release.yml ps -q file-sharing)
  STATUS=$(docker inspect "$CID" --format='{{.State.Health.Status}}' 2>/dev/null || true)
  if [ "${STATUS}" = "healthy" ]; then
    echo "[release] Isolated dev service is healthy."
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "[release] ERROR: isolated dev service never became healthy."
    docker compose -p "$RID" -f docker-compose.dev.yml -f docker-compose.release.yml logs --tail=20
    false
  fi
  sleep 2
done
trap - ERR
```

The `ERR` trap covers stack creation and the health wait. Later blocks
have image-cleanup traps. If any later gate fails, run the stack teardown
block below before stopping; do not continue to versioning or publishing.

If Docker-out-of-Docker cannot access the checkout or build context, stop and
report the environment limitation rather than treating the smoke result as a
valid release gate.

### 3. Run Tests

All release gates must pass. Stop and report on the first failure —
do not proceed with the release.

`TESTED_SHA` was pinned in step 1, before any build. Re-export it (and
`RID`/`RELEASE_RID`) literally at the top of every block below.

```bash
set -euo pipefail
TESTED_SHA="<sha-from-step-1>"

# === Gate 1. Go unit tests (reuses Dockerfile.go-test) ===
# gofmt, vet, and tests are RUN steps, so a failure fails the build
# (fail-closed). --no-cache forces re-execution: without it, an unchanged
# tree could re-tag cached layers and report a stale pass.
echo "[release] Running Go unit tests in container..."
TEST_IMAGE="simple-fileurl-release-test:${TESTED_SHA:0:12}"
trap 'docker image rm "$TEST_IMAGE" >/dev/null 2>&1 || true' ERR
docker build --pull --no-cache --tag "$TEST_IMAGE" --file Dockerfile.go-test .
echo "[release] Running installer checks in container..."
docker run --rm "$TEST_IMAGE" bash test/install-upgrade.sh
docker image rm "$TEST_IMAGE" >/dev/null
trap - ERR
```

```bash
set -euo pipefail
# === Gate 2. SFTP contract tests (mirrors CI, containerized) ===
# cd /src pins the working directory: the image WORKDIR happens to be /src
# today, but the relative test/ paths must not depend on that.
echo "[release] Running SFTP contract tests in container..."
SFTP_TEST_IMAGE="simple-fileurl-release-sftp-test:$$"
trap 'docker image rm "$SFTP_TEST_IMAGE" >/dev/null 2>&1 || true' ERR
docker build --tag "$SFTP_TEST_IMAGE" --file Dockerfile.sftp-test .
docker run --rm "$SFTP_TEST_IMAGE" sh -c 'cd /src && sh test/sftp-config.sh && sh test/sftp-reconcile.sh'
docker image rm "$SFTP_TEST_IMAGE" >/dev/null
trap - ERR
```

```bash
set -euo pipefail
TESTED_SHA="<sha-from-step-1>"
# === Gate 3. Compose validation (mirrors CI, production config) ===
echo "[release] Validating compose files..."
HOST_SHARE_PATH=/tmp/ci-share SHARE_PREFIX=files PUBLIC_URL=https://example.test \
  ADMIN_TOKEN="ci-${TESTED_SHA:0:12}" IMAGE_TAG="sha-${TESTED_SHA}" \
  SFTP_ADMIN_PASSWORD=dummy \
  docker compose -f docker-compose.yml config > /dev/null
RELEASE_RID="config-check" \
  docker compose -f docker-compose.dev.yml -f docker-compose.release.yml config > /dev/null
```

```bash
set -euo pipefail
RID="release-<same-pid-as-step-2>"
TESTED_SHA="<sha-from-step-1>"
export RELEASE_RID="$RID"
# === Gate 4. Smoke test against the isolated stack (containerized) ===
# The runner attaches to the isolated project network and reaches the
# service by its Compose service name — no host curl, no hardcoded
# container name, no published host ports, no assumed localhost
# reachability. Service env (PUBLIC_URL, admin settings, hash settings)
# is pinned by docker-compose.release.yml.
echo "[release] Running smoke test in container..."
SMOKE_IMAGE="simple-fileurl-release-smoke:$$"
trap 'docker image rm "$SMOKE_IMAGE" >/dev/null 2>&1 || true' ERR
docker build --tag "$SMOKE_IMAGE" --file Dockerfile.smoke .
docker run --rm --network "${RID}_default" \
  -e BASE=http://file-sharing:8080 \
  -e LINK_PREFIX=http://file-sharing:8080 \
  -e ADMIN_TOKEN=dummy \
  "$SMOKE_IMAGE"
docker image rm "$SMOKE_IMAGE" >/dev/null
trap - ERR
```

After all gates, verify the tested tree. Regardless of this check or any
gate failing, execute the separate teardown block below before stopping:

```bash
set -euo pipefail
RID="release-<same-pid-as-step-2>"
TESTED_SHA="<sha-from-step-1>"
export RELEASE_RID="$RID"
test -z "$(GIT_MASTER=1 git status --short)" || { echo "[release] ERROR: working tree dirtied during tests. Stop and inspect." >&2; exit 1; }
if [ "$(GIT_MASTER=1 git rev-parse HEAD)" != "$TESTED_SHA" ]; then
  echo "[release] ERROR: HEAD moved during tests (want $TESTED_SHA). Re-run steps 2-3." >&2
  exit 1
fi
```

Always tear down only this run's stack, including on gate failure:

```bash
set -euo pipefail
RID="release-<same-pid-as-step-2>"
export RELEASE_RID="$RID"
docker compose -p "$RID" -f docker-compose.dev.yml -f docker-compose.release.yml down -v --remove-orphans
docker image rm "simple-fileurl-release:$RID" "simple-fileurl-release-sftp:$RID" "simple-fileurl-release-sftp-admin:$RID" "simple-fileurl-release-share:$RID" >/dev/null 2>&1 || true
echo "[release] stack $RID removed."
```

### 4. Determine Current and Next Version

```bash
GIT_MASTER=1 git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"
```

Analyze `git log` since the last tag (subjects for type prefixes, full
bodies for footers):

- **MAJOR**: any subject matches `type!:` / `type(scope)!:` (e.g. `feat!:`),
  or `BREAKING CHANGE` appears anywhere in a commit message body (footer)
- **MINOR**: any commit starts with `feat:` or `feat(`
- **PATCH**: `fix:`, `docs:`, `style:`, `refactor:`, `perf:`, `test:`, `ci:`, `chore:`, or anything else

```bash
LAST_TAG=$(GIT_MASTER=1 git describe --tags --abbrev=0 2>/dev/null || echo "")
if [ -n "$LAST_TAG" ]; then RANGE="${LAST_TAG}..HEAD"; else RANGE="HEAD"; fi
GIT_MASTER=1 git log "$RANGE" --format=%s --no-merges | grep -Eq '^[A-Za-z]+(\(.+\))?!:' && echo "MAJOR (subject)" || true
GIT_MASTER=1 git log "$RANGE" --format=%B --no-merges | grep -q "BREAKING CHANGE" && echo "MAJOR (footer)" || true
```

Calculate the next version (e.g. `v0.1.0`). If no previous tag exists,
use `v0.0.0` as the base and cut the first release as `v0.0.1`.

### 5. Generate Release Notes

```bash
LAST_TAG=$(GIT_MASTER=1 git describe --tags --abbrev=0 2>/dev/null || echo "")
if [ -n "$LAST_TAG" ]; then
  GIT_MASTER=1 git log "${LAST_TAG}..HEAD" --oneline --no-merges
else
  GIT_MASTER=1 git log --oneline --no-merges
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

### 6. Prepare and Confirm the Release

Before asking for confirmation, refresh the remote state and verify that the
release still describes the current `main` commit:

```bash
set -euo pipefail
TESTED_SHA="<sha-from-step-1>"
GIT_MASTER=1 git fetch origin main
test "$(GIT_MASTER=1 git branch --show-current)" = main
test -z "$(GIT_MASTER=1 git status --short)"

read -r BEHIND AHEAD <<EOF
$(GIT_MASTER=1 git rev-list --left-right --count origin/main...HEAD)
EOF
test "$BEHIND" = 0 || { echo "[release] ERROR: local main is behind origin/main" >&2; exit 1; }

# The gates in step 3 certified $TESTED_SHA. If HEAD moved since (new
# commit, rebase, checkout), the gates no longer describe this tree.
if [ "$(GIT_MASTER=1 git rev-parse HEAD)" != "$TESTED_SHA" ]; then
  echo "[release] ERROR: HEAD moved since tests ran (want $TESTED_SHA). Re-run steps 2-3." >&2
  exit 1
fi

RELEASE_TAG="{VERSION}"
case "$RELEASE_TAG" in v*) ;; *) RELEASE_TAG="v$RELEASE_TAG" ;; esac
printf '%s\n' "$RELEASE_TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'

# Explicit existence checks with strict exit codes. (Do NOT use `! cmd`
# here: under `set -e` a negated command never fails the script, so a taken
# tag would slip through. Do NOT treat any failure as absence either:
# git distinguishes "ref missing" from "command failed".)
set +e
GIT_MASTER=1 git rev-parse --quiet --verify "refs/tags/$RELEASE_TAG" >/dev/null 2>&1
LOCAL_ST=$?
GIT_MASTER=1 git ls-remote --exit-code --tags origin "refs/tags/$RELEASE_TAG" >/dev/null 2>&1
REMOTE_ST=$?
set -e
if [ "$LOCAL_ST" -eq 0 ]; then
  echo "[release] ERROR: tag $RELEASE_TAG already exists locally." >&2
  exit 1
elif [ "$LOCAL_ST" -ne 1 ]; then
  echo "[release] ERROR: local tag check failed (status $LOCAL_ST), not verified absent. Aborting." >&2
  exit 1
fi
if [ "$REMOTE_ST" -eq 0 ]; then
  echo "[release] ERROR: tag $RELEASE_TAG already exists on origin." >&2
  exit 1
elif [ "$REMOTE_ST" -ne 2 ]; then
  # ls-remote --exit-code reports 2 for "no matching refs"; anything else
  # (e.g. 128: network/auth failure) means absence was NOT established.
  echo "[release] ERROR: remote tag check inconclusive (status $REMOTE_ST), not verified absent. Aborting." >&2
  exit 1
fi
echo "[release] release $RELEASE_TAG on $TESTED_SHA is clear to confirm."
```

Present the calculated version, release notes, current commit, and exact tag
to be pushed. Ask for explicit confirmation before tagging or pushing. If any
working-tree or remote state changes after this check, repeat the check.

### 7. Tag and Push

Confirmation may have taken time, so repeat the full step 6 validation
after confirmation — branch, clean tree, fetch, behind-check, semver shape,
HEAD-equals-TESTED_SHA, and both strict tag checks — then tag and push:

```bash
set -euo pipefail
RELEASE_TAG="{VERSION}"
TESTED_SHA="<sha-from-step-1>"
case "$RELEASE_TAG" in v*) ;; *) RELEASE_TAG="v$RELEASE_TAG" ;; esac
printf '%s\n' "$RELEASE_TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'
GIT_MASTER=1 git fetch origin main
test "$(GIT_MASTER=1 git branch --show-current)" = main
test -z "$(GIT_MASTER=1 git status --short)"
read -r BEHIND AHEAD <<EOF
$(GIT_MASTER=1 git rev-list --left-right --count origin/main...HEAD)
EOF
test "$BEHIND" = 0 || { echo "[release] ERROR: local main is behind origin/main" >&2; exit 1; }
if [ "$(GIT_MASTER=1 git rev-parse HEAD)" != "$TESTED_SHA" ]; then
  echo "[release] ERROR: HEAD moved during confirmation (want $TESTED_SHA). Re-run steps 2-3." >&2
  exit 1
fi
set +e
GIT_MASTER=1 git rev-parse --quiet --verify "refs/tags/$RELEASE_TAG" >/dev/null 2>&1
LOCAL_ST=$?
GIT_MASTER=1 git ls-remote --exit-code --tags origin "refs/tags/$RELEASE_TAG" >/dev/null 2>&1
REMOTE_ST=$?
set -e
if [ "$LOCAL_ST" -eq 0 ]; then
  echo "[release] ERROR: tag $RELEASE_TAG already exists locally." >&2
  exit 1
elif [ "$LOCAL_ST" -ne 1 ]; then
  echo "[release] ERROR: local tag check failed (status $LOCAL_ST). Aborting." >&2
  exit 1
fi
if [ "$REMOTE_ST" -eq 0 ]; then
  echo "[release] ERROR: tag $RELEASE_TAG already exists on origin." >&2
  exit 1
elif [ "$REMOTE_ST" -ne 2 ]; then
  echo "[release] ERROR: remote tag check inconclusive (status $REMOTE_ST). Aborting." >&2
  exit 1
fi
# Tag the tested commit explicitly (HEAD could in principle move between
# the check above and the tag; naming the SHA keeps the tag exact).
GIT_MASTER=1 git tag -a "$RELEASE_TAG" "$TESTED_SHA" -m "Release $RELEASE_TAG"
# Push the tested SHA as main plus the tag, with full refspecs, in one
# atomic operation. The branch source is the pinned $TESTED_SHA — not the
# moving local main — so exactly the reviewed state lands upstream.
# --atomic keeps a partial push from publishing main without its tag (or
# vice versa); if the server ever rejects --atomic, stop and report instead
# of pushing the refspecs separately.
GIT_MASTER=1 git push --atomic origin "$TESTED_SHA:refs/heads/main" "refs/tags/$RELEASE_TAG:refs/tags/$RELEASE_TAG"
```

Pushing a `v*` tag triggers the CI `push` job, which publishes
`ghcr.io/tryweb/simple-fileurl:{tag}`, the matching `-sftp` and
`-sftp-admin` images, plus `sha-` tags for each image.

### 8. Report

After push, inform the user:
- New version tag
- GHCR image URLs: `ghcr.io/tryweb/simple-fileurl:{version}`,
  `ghcr.io/tryweb/simple-fileurl-sftp:{version}`, and
  `ghcr.io/tryweb/simple-fileurl-sftp-admin:{version}`
- CI workflow run URL for the tag build

## Rules

- Never skip the test step (Go unit AND installer AND SFTP contract AND
  compose validation AND smoke)
- Never release with uncommitted changes (must commit first)
- Never push without user confirmation
- Never tag HEAD: tag the recorded `$TESTED_SHA` (pinned in step 1, before
  any build), and abort if `HEAD` moved
- Never check tag existence with `! cmd` under `set -e` (use explicit `if`
  with strict exit codes: rev-parse absence is 1, ls-remote absence is 2,
  anything else aborts)
- Never use the default dev Compose project for release verification; always
  `-p release-<id>` with `docker-compose.release.yml` (no published host
  ports, per-run image names, pinned env), and tear down only that project
  afterwards; gate blocks trap `ERR` cleanup so failures do not leak stacks
  or images
- If `git log` is empty since the last tag, warn the user
- Use semver format: `v{MAJOR}.{MINOR}.{PATCH}`
