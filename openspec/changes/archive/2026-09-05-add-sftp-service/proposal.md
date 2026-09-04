## Why

目前部署依賴外部既有的 SFTP 主機提供檔案來源，`HOST_SHARE_PATH` 只是指向那份外部資料目錄。本專案自身沒有 SFTP 寫入入口，想上傳檔案必須繞過 Compose 另行處理。這項變更在 Compose 裡增加一個與 web 服務共用同一 Host 目錄的 SFTP 容器，讓上傳與分享下載形成閉環，並附帶一個 Admin Web UI 容器負責 SFTP 開戶與 SSH key 產生，讓不熟悉 `ssh-keygen` 的管理者也能操作。

## What Changes

- 在 `docker-compose.yml`（與 `docker-compose.dev.yml` 對應調整）新增 `sftp` 服務，與 `file-sharing` 共用同一個 `HOST_SHARE_PATH` bind source。
- SFTP 容器以 read-write 模式掛載同一目錄到 root-owned `/share`，以 `/share` 作為 chroot；只有 `/share/${SHARE_PREFIX}` 子樹可寫，使用者登入後看到的頂層樹與 web 服務的 logical path 一致。
- 認證僅支援公鑰（`authorized_keys`），不啟用密碼登入；使用者與公鑰由 Admin UI 管理（可選的第一個 SFTP user/key 由 seed 變數提供，見 design.md）。
- 新增 `sftp-admin` 服務：Web 管理介面，可建立／停用 SFTP 使用者、為使用者產生 SSH keypair（私鑰僅以 single-use 方式下載）、登記外部公鑰、列出使用者；Admin 只寫入共用的 user manifest，不直接修改 SFTP 容器的 `/etc/passwd`。
- SFTP 服務發布標準 22 port（Host 側可配置，例如 `SFTP_PORT`，預設 2222 避免與 Host sshd 衝突）；Admin UI 發布獨立 Host port（例如 `SFTP_ADMIN_PORT`，預設 8081）。
- `.env.example` 新增 SFTP 與 Admin 相關變數與說明；README 新增 SFTP 使用與管理段落。
- SFTP 寫入後 web 服務的 hash URL 按現有規則自然生效（內容 hash 隨檔案內容變化），不改 `file-sharing` 行為。

## Capabilities

### New Capabilities

- `sftp-service`: 以獨立容器提供 SFTP 上傳／管理入口，與 web 分享服務共用同一 Host 目錄，僅支援公鑰認證。
- `sftp-admin`: 以獨立容器提供 SFTP 使用者與 SSH key 的 Web 管理介面（含開戶、keypair 產生、停用）。

### Modified Capabilities

無。`file-sharing` 的 REQUIREMENTS 不變（web 服務維持 `:ro` 掛載與現有 hash 規則）。

## Impact

- 新增 Compose `sftp` 服務（自建 Alpine + OpenSSH 鏡像，見 design.md）、`sftp-admin` 服務與 `sftp-users` named volume；兩容器以 user manifest 交換使用者狀態，並各自提供健康檢查。
- 新增環境變數：`SFTP_PORT`、`SFTP_ADMIN_PORT`、`SFTP_ADMIN_PASSWORD`、`SFTP_GID`、可選的 `SFTP_SEED_USER`/`SFTP_SEED_PUBKEY`；`HOST_SHARE_PATH` 仍是 web 與 SFTP 共用的資料根，但 SFTP 額外要求其根目錄符合 OpenSSH chroot ownership contract。
- 安全面：公鑰認證、root-owned chroot、`ForceCommand internal-sftp`、轉送功能關閉與目錄穿越防護；Admin UI 是特權入口，預設只綁 localhost、必須以管理密碼保護，生產環境需經 TLS；私鑰不落盤、不進 log、只可下載一次；可寫入意味著上傳內容會直接影響分享連結（`file` 模式下內容變則 hash 變）。
- Dev/CI：`docker-compose.dev.yml` 以 repo-local `testdata` 掛載 SFTP 容器，使用測試 user manifest 與測試 key；測試不得把 production key 或 admin password 帶入 image/repository。
