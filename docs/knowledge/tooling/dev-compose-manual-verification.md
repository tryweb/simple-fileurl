# Dev Compose 手動驗證流程

## Context

本專案可使用 `docker-compose.dev.yml` 建立完整的本地驗證環境，包含
file-sharing、SFTP、SFTP admin，以及只在首次啟動時執行的 fixture 初始化器。

## Problem

只確認 image build 成功，無法驗證 container health、管理登入、shared
config 權限、live settings 更新，以及未授權請求是否被拒絕。Production
Compose 又要求必要的環境變數，不適合直接作為本地手動測試入口。

## Solution

使用 dev Compose 建置並啟動環境：

```bash
docker compose -f docker-compose.dev.yml config
docker compose -f docker-compose.dev.yml build --no-cache
docker compose -f docker-compose.dev.yml up -d --force-recreate
docker compose -f docker-compose.dev.yml ps
```

預期 `file-sharing`、`sftp`、`sftp-admin` 都是 `healthy`；`share-init`
顯示 `Exited (0)` 是正常結果。

基本 healthcheck：

```bash
curl http://localhost:18080/healthz
curl http://localhost:18081/healthz
```

兩者都應回傳 `{"status":"ok"}`。

確認 shared config 的 least-privilege mount：

```bash
docker compose -f docker-compose.dev.yml exec file-sharing \
  sh -c 'cat /etc/app/config.json >/dev/null && ! touch /etc/app/write-test'
docker compose -f docker-compose.dev.yml exec sftp-admin \
  ls -l /etc/app/config.json
```

`file-sharing` 應能讀取但不能寫入，`sftp-admin` 應能寫入；config 通常是
`0640 root:webreaders`。

管理 UI 手動流程：

1. 開啟 `http://localhost:18081/login`。
2. Dev 預設密碼是 `dev-admin-password`。
3. 確認 Users、Settings、Sign out navigation 都存在。
4. 在 Settings 驗證五個欄位、錯誤輸入、secret 欄位不顯示原值。
5. 將 `PUBLIC_URL` 或 `HASH_ALGORITHM` 改變後，不重啟 container，呼叫分享 API
   驗證新設定立即生效。
6. 輪替 `SFTP_ADMIN_PASSWORD`，確認舊密碼失效、新密碼有效，完成後還原
   `dev-admin-password`。

API 與認證 smoke test：

```bash
curl -i -X POST http://localhost:18081/login \
  --data-urlencode 'password='

curl -i http://localhost:18080/api/links

# ADMIN_AUTH_HEADER 應由測試環境設定為完整的管理 API 認證 header。
curl -i http://localhost:18080/api/links \
  -H "$ADMIN_AUTH_HEADER"
```

空密碼登入應回 HTTP 200 並顯示 `Wrong password`，不建立有效 session；未帶
Bearer token 的 links API 應回 HTTP 401。

若要測試 legacy invalid SSH key，先備份
`/var/lib/sftp-users/users.json`，寫入一個 invalid authorized key，重新整理
Users 頁面，確認 HTTP 200 並顯示 `(invalid)`，最後一定要還原 manifest。

## Why It Works

Dev Compose 會用 `share-init` 將 repository fixture 放入 `share-data`，並以
named volumes 模擬 production 的 `app-config`、`file-links` 與 `sftp-users`
跨 container 邊界。`file-sharing` 的 `/etc/app` 是 read-only，admin 是
read-write，因此手動測試可同時驗證服務協作與權限隔離。

## Side Effects / Tradeoffs

- `build --no-cache` 會重新建置所有 dev images，速度比一般 build 慢。
- Settings、users 與 links 測試會修改 named volumes；測試後應還原設定與 manifest。
- `docker compose down` 不會刪除 volumes；若要完全重置測試資料才使用 `down -v`。
- 在 Docker-out-of-Docker 環境中，若目前 shell 無法直接連到 published localhost
  port，應從同一 Compose network 的暫時 container 內執行 curl。

## Evidence

- `docker compose -f docker-compose.dev.yml build --no-cache`：四個 dev images 建置成功。
- 三個長駐服務 healthcheck 通過；`share-init` 正常退出。
- `/etc/app/config.json` 實測為 `0640 root:webreaders`；file-sharing 可讀但寫入被
  `Read-only file system` 拒絕。
- `/healthz` 回 HTTP 200；空密碼登入被拒絕；未授權 links API 回 HTTP 401。
- Go 自動化 gate：`gofmt`、`go vet ./...`、
  `go test -race -shuffle=on -count=1 ./...` 通過。

## Related Files

- `docker-compose.dev.yml`
- `Dockerfile`
- `Dockerfile.sftp`
- `Dockerfile.sftp-admin`
- `Dockerfile.dev-share`
- `Dockerfile.go-test`
- `docs/knowledge/tooling/sftp-ci-verification.md`

## Tags

- docker-compose
- dev
- manual-testing
- smoke-test
- sftp
- settings
