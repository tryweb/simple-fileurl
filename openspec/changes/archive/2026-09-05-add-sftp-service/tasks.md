## 1. SFTP 容器與資料契約

- [x] 1.1 新增 `Dockerfile.sftp`、最小 `sshd_config` 與 entrypoint，固定 `PasswordAuthentication no`、`KbdInteractiveAuthentication no`、`ForceCommand internal-sftp`、chroot `/share`、禁止 PTY/轉送，並以 `sshd -T` 驗證有效設定
- [x] 1.2 實作 SFTP 啟動前檢查，驗證 `/share` 是 root-owned、非 unprivileged-writable 目錄，`/share/${SHARE_PREFIX}` 存在且具 `SFTP_GID` group write/traverse 權限，以錯誤 ownership/mode 使容器 non-zero 結束來驗證
- [x] 1.3 定義 `sftp-users/users.json` schema 與 atomic-write/reconcile 流程，讓 SFTP 容器輪詢 manifest、建立 enabled Linux users、生成 chroot 外的 `authorized_keys.d/<user>`（`root:sftpusers 0640`，SFTP users 不可寫），並以「新增 user 後不重啟即可 key login」驗證
- [x] 1.4 實作 disabled user 的 reconcile，移除或失效其 authorized key 且不影響其他 user，以停用後新連線失敗、其他 user 成功來驗證
- [x] 1.5 設定 `SFTP_GID`、setgid prefix、`umask 0002` 與等價 `0664/0775` 的新檔案／目錄權限，以 `stat` 驗證 web 非 root process 可讀取與 traversable

## 2. Compose 與部署配置

- [x] 2.1 在 `docker-compose.yml` 新增 `sftp` 服務，將 `${HOST_SHARE_PATH}` 以 `:rw` 掛到 `/share`、掛載 `sftp-users`、發布 `${SFTP_PORT:-2222}:22`、加入 SSH healthcheck 與 restart，以 `docker compose config` 驗證 web 仍是 `:ro` 且兩服務使用同一 Host source
- [x] 2.2 在 `.env.example` 新增 `SFTP_PORT`、`SFTP_GID`、`SFTP_ADMIN_PORT`、`SFTP_ADMIN_PASSWORD`、可選 `SFTP_SEED_USER`/`SFTP_SEED_PUBKEY`，以缺少 SFTP share 設定與缺少 Admin password 的分別 fail-fast 行為驗證容器責任邊界
- [x] 2.3 在 `docker-compose.dev.yml` 新增一次性 fixture init service，將 `./testdata` 複製為 root-owned `share-data` named volume，再由 web/SFTP 共用該 volume；另加入 `sftp-users`、測試 manifest/key 與固定測試變數，以 `docker compose -f docker-compose.dev.yml config` 驗證 dev mount/port/依賴無歧義

## 3. Admin UI 與 user lifecycle

- [x] 3.1 新增 `Dockerfile.sftp-admin` 與 Go Admin UI，manifest writer 以 container root 寫入 `sftp-users`、SFTP 端以受限 volume 讀寫 manifest，且 Admin 不掛載 Host share，以容器 mount inspection 驗證 least-privilege 邊界
- [x] 3.2 實作 `SFTP_ADMIN_PASSWORD` fail-fast、constant-time password compare、隨機 HttpOnly/SameSite session cookie、30 分鐘 expiry、logout、CSRF token 與 failed-login rate limit，以未登入/錯密碼/過期 session/重試情境的 HTTP 測試驗證
- [x] 3.3 實作使用者建立與 `ed25519` keypair 產生，將 private key 以 single-use download response 傳出後刪除暫存檔，且不寫入 manifest、persistent volume 或 log，以建立後下載一次、第二次 404/拒絕及 volume/log inspection 驗證
- [x] 3.4 實作 username regex、reserved-name、duplicate-user 與 malformed-key 驗證，以每種無效輸入均回應明確錯誤且 manifest 不變來驗證
- [x] 3.5 實作外部 public key 登記、fingerprint 顯示、idempotent dedup 與多 key 保留，以同一 user 註冊兩把 key 並分別 login 來驗證
- [x] 3.6 實作 user list、disable/revoke 與 atomic manifest update，以 revoke 後該 user 無法新登入、其他 user 不受影響且 SFTP 無需重啟來驗證
- [x] 3.7 在 `docker-compose.yml` 與 `docker-compose.dev.yml` 加入 `sftp-admin`（預設只發布 `127.0.0.1:${SFTP_ADMIN_PORT:-8081}:8080`、共用 `sftp-users`、healthcheck），以 `docker compose config` 與未設定 password 的 non-zero 啟動測試驗證

## 4. 端到端安全與分享驗證

- [x] 4.1 以測試 key 經 Admin UI 建立 user，等待 manifest reconcile 後經 SFTP 登入並上傳 `files/mydir/report.pdf`，以 `sftp put`、`ls` 與不重啟條件驗證完整 lifecycle
- [x] 4.2 以 web hash URL 下載剛上傳的檔案，以 `curl` 回傳 HTTP 200 且 bytes 一致來驗證 web read-only mount 與 SFTP upload permission contract
- [x] 4.3 嘗試 password/keyboard-interactive login、PTY、TCP/X11 forwarding、`../` 與 `/etc/hostname`，以全部被拒絕或只在 `/share` chroot 內作用來驗證 session hardening
- [x] 4.4 驗證 `${HOST_SHARE_PATH}` 非 root-owned/writable、prefix 不存在、prefix group GID 不符等部署錯誤，以 SFTP container fail fast 且保留既有資料來驗證
- [x] 4.5 驗證 `SFTP_SEED_*` 只在 manifest 首次不存在時生效，既有 manifest 不被覆蓋，以兩次啟動後使用者/key 狀態不漂移來驗證

## 5. 文件與規格收尾

- [x] 5.1 更新 `README.md`，說明 Host ownership/mode 前置條件、SFTP `sftp -P` 操作、Admin 開戶、TLS/private-network 要求、私鑰一次性下載、hash link 漂移與 `sftp-users` backup/restore，以文件步驟可在 dev compose 重現來驗證
- [x] 5.2 執行 `openspec validate add-sftp-service --strict`、相關 Go tests、Compose config 與完整 dev smoke test，以所有命令 exit 0 且列出的端到端情境通過來驗證
