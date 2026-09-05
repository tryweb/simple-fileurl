## Why

目前所有 SFTP 使用者共用同一個 `SHARE_PREFIX`（例如 `files/`）目錄，任何使用者都能看到、修改、刪除其他使用者上傳的檔案。當多個使用者需要各自獨立的上傳空間時，現有的單一 namespace 架構無法提供隔離。需要支援 per-user 獨立目錄，讓每個使用者擁有自己的命名空間，同時保留現有的 `files/` 作為所有人共用目錄。

## What Changes

- **新增 per-user 目錄**：SFTP reconcile 在建立使用者時，同時在 chroot 下建立 `/share/<username>/` 目錄，owner 為該使用者，group 為 `WEB_GID`，mode `0750`，僅該使用者可寫入，web 服務透過 WEB_GID 可讀取，其他 SFTP 使用者無法存取
- **保留共用目錄**：現有的 `SHARE_PREFIX`（例如 `files/`）維持 group-writable，所有使用者共用，行為不變
- **Web 服務掃描多個 namespace**：不再只掃描 `/opt/sharefiles/<SHARE_PREFIX>`，改為掃描 `/opt/sharefiles/` 下的所有頂級目錄（`files/`、`<username>/` 等），每個目錄各自形成一個邏輯命名空間
- **URL 格式不變**：`/{dirHash}/{fileHash}` 保持不變，因為不同目錄的 logical path 不同（如 `files/docs` vs `jonathan/docs`），hash 自然不同
- **Admin listing 不變**：本次不改動 admin listing 的顯示邏輯，繼續列出所有可分享檔案

## Capabilities

### New Capabilities

（無新增 capability）

### Modified Capabilities

- `file-sharing`：NamespaceRoot 從單一 `SHARE_PREFIX` 目錄擴展為掃描 container root 下的多個頂級目錄，每個目錄各自形成獨立的 logical namespace。下載路由、hash 計算、安全邊界檢查需配合多 namespace 調整。
- `sftp-service`：Runtime user reconciliation 需在建立使用者時同步建立 `/share/<username>/` 個人目錄，設定正確的 owner/mode，使該目錄僅該使用者可寫入。共用 `SHARE_PREFIX` 目錄維持現有 group-writable 行為。

## Impact

- **Web 服務**（`internal/store/store.go`、`internal/config/config.go`）：`NamespaceRoot()` 需改為回傳多個 scan root，`scan()` 需遍歷多個目錄，logical path 計算需反映實際的頂級目錄名稱
- **SFTP reconcile**（`sftp/reconcile.sh`）：`ensure_account` 需新增建立 per-user 目錄的邏輯，設定 `chown <user>:<user>` 和 `chmod 0700`
- **SFTP entrypoint**（`sftp/entrypoint.sh`）：`validate_share` 需允許 per-user 目錄存在，不需要預先建立（由 reconcile 動態建立）
- **Docker Compose**：web 服務掛載可能需要從 `ro` 調整，或保持 `ro` 因為 web 只需要讀取
- **測試**：需新增 per-user 目錄的整合測試，驗證隔離性和 web 服務的多 namespace 掃描
- **向後相容**：現有的 `files/` 目錄和 URL 完全不受影響，既有的分享連結繼續有效
