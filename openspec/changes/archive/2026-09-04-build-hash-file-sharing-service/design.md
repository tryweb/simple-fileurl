## Context

本專案目前只有 OpenSpec 規格骨架，沒有既有服務或可沿用的 web framework。設計需支援 Alpine 容器、Docker Compose 啟動、唯讀檔案掛載，並將 hash URL 解析與主機檔案系統隔離；完整動機與範圍見 `proposal.md`，可觀察行為見 `specs/file-sharing/spec.md`。

## Goals / Non-Goals

**Goals:**

- 以單一 stateless web service 提供檔案列表、分享網址產生與下載。
- 讓同一組 logical path、hash target 與演算法產生可重現的十六進位 hash URL。
- 在每次解析與開啟檔案時維持分享根目錄邊界，並以非 root 使用者執行。
- 使用無外部 runtime dependency 的 Alpine image，降低部署與維護成本。
- 讓檔案在服務啟動後新增、刪除或變更時，下一次掃描能反映最新狀態。

**Non-Goals:**

- 登入、使用者權限、隨機 token、URL 到期與撤銷機制。
- 上傳、刪除、修改或重新命名檔案。
- 多租戶、資料庫、分散式索引與跨容器共享狀態。
- TLS termination；`PUBLIC_URL` 可為 HTTPS，但 TLS 應由反向代理或 ingress 負責。

## Decisions

### 使用 Go 標準函式庫建立單一服務

採用 Go 的 `net/http`、`crypto/md5`、`crypto/sha256`、`filepath` 與 `html/template`，不引入 web framework 或資料庫。Go 可編譯成單一靜態執行檔，適合 multi-stage build 後放入 Alpine；相比 Node/Python 可省略 runtime 依賴，亦比引入框架更符合目前功能規模。

### 以即時檔案掃描建立短生命週期索引

服務不保存檔案 metadata 到資料庫；在首頁產生列表時掃描 `/opt/sharefiles/SHARE_PREFIX`，在下載解析時使用當次掃描的索引，必要時以檔案大小與修改時間做 process-local hash cache。這讓檔案變更不需要重啟服務，也避免持久化索引與檔案同步失敗；代價是大量檔案或大型檔案的首次掃描會消耗 I/O 與 CPU，第一版以單機中小型分享目錄為目標。

索引項目至少包含：logical namespace 下的 normalized relative directory、basename、regular-file 狀態、hash target 對應的識別值與完整相對路徑。物理檔案路徑與 logical path 分開保存；hash 輸入固定如下：

- 目錄：從 `SHARE_PREFIX` 開始的 slash-separated SFTP logical path；prefix 根目錄本身也納入 hash，例如 prefix 為 `files` 時，根目錄的輸入是 `files`。
- `HASH_TARGET=file`：檔案原始 bytes。
- `HASH_TARGET=filename`：檔案 basename 的 UTF-8 字串。
- 輸出：小寫 hexadecimal，作為 URL path segment。

### 只接受兩個 hash path segments

`GET /<directory-hash>/<file-hash>` 是唯一下載格式；服務不把使用者輸入的原始路徑直接拼接到檔案系統。resolver 先以 directory hash 找候選目錄，再以 file hash 在該目錄內找候選 regular file；找不到回 404，多個候選回 deterministic conflict（HTTP 409），不任意選檔。

首頁 `GET /` 使用 HTML template 顯示相對路徑、當前設定與完整 `PUBLIC_URL` link。頁面只提供下載連結，不提供會改變檔案的操作；template 必須自動 escape 檔名與相對路徑。

### 以拒絕所有 symlink 簡化檔案邊界

掃描時跳過 symlink，下載前再次確認目標是 regular file，並透過 clean absolute path、`Rel` 邊界檢查與 `EvalSymlinks` 驗證目標仍在 `/opt/sharefiles/SHARE_PREFIX` 內。拒絕 namespace 內部 symlink 雖比「允許安全的內部 symlink」更嚴格，但可降低 TOCTOU 與 symlink race 風險，且不影響主要分享用途。

所有檔案以唯讀方式開啟並串流回應；服務程序不具備寫入分享根目錄的權限。Unexpected filesystem errors 記錄 server-side 細節，但回應不包含絕對路徑。

### 以 Compose bind mount 與環境變數分離物理路徑和 logical path

Compose 將部署者指定的 Host bind source `${HOST_SHARE_PATH}` 掛載到固定的 Container path `/opt/sharefiles:ro`。`HOST_SHARE_PATH` 只供 Compose 做 volume interpolation，不傳入 hash resolver，也不會出現在 URL 或錯誤回應中。服務從 `SHARE_PREFIX` 取得 SFTP 使用者看到的 logical path 前綴，並只掃描 `/opt/sharefiles/SHARE_PREFIX`；因此 Host 路徑與 Container mount path 都不會成為 hash 輸入。

使用者可依環境填寫 `.env`；以下是示意值，不是程式硬編碼的部署值：

```env
HOST_SHARE_PATH=/path/to/sftp-root
SHARE_PREFIX=files
PUBLIC_URL=https://weurl.everplast.net
HASH_TARGET=file
HASH_ALGORITHM=md5
PORT=8080
```

Compose 的 volume 目標 `/opt/sharefiles` 是服務內部固定契約，不需要部署者另設 `SHARE_ROOT`。`HASH_TARGET` 僅接受 `file` 或 `filename`；`HASH_ALGORITHM` 僅接受 `md5` 或 `sha256`。`PORT` 是部署便利性的附加設定，不改變 URL hash 行為。PUBLIC_URL 在產生連結前移除多餘尾斜線。

在 SFTP 結構的示意中，若 Host source 是包含 `SHARE_PREFIX` 的任意資料目錄，且 `SHARE_PREFIX=files`，則 `files/mydir/cron.txt` 對應到 `/opt/sharefiles/files/mydir/cron.txt`；Host source 只是一個部署設定，不是預設值。若 SFTP 顯示的是 chroot 或 virtual alias，`HOST_SHARE_PATH` 必須填實際 Host data directory，不能直接把 SFTP 顯示路徑當作 Host path。

### Alpine multi-stage image 與非 root執行

Dockerfile 使用 Go builder stage 編譯服務，再將 binary 與必要的 CA certificates 放入 Alpine runtime image。runtime 建立固定非 root 使用者，Compose 以 read-only bind mount 掛載分享目錄，並以 `/healthz` 作為 container healthcheck。HTTP 預設綁定 `:8080`；對外 HTTPS 與網域路由由部署環境的 reverse proxy 處理。

## Risks / Trade-offs

- **大型目錄掃描延遲** → 以 metadata hash cache 降低未變更檔案成本，並將初版目標限定為單機中小型分享目錄；以整合測試確認 timeout 與錯誤行為。
- **大型檔案 content hash 需要讀完整檔案** → 只在 `file` 模式計算內容 hash，下載採串流；後續若規模需要可加入持久化索引，但不提前引入。
- **bearer link 可被轉發** → 在文件中明確標示持有 URL 即可下載；安全敏感場景需在反向代理或後續 change 加入認證／到期 token。
- **檔案在索引後被移除或替換** → 開檔前重新檢查狀態與根目錄邊界，失敗時回 404/5xx 並不暴露路徑；不可保證在 filesystem race 下提供快照語意。
- **MD5 不適合抗碰撞安全用途** → 保留 MD5 僅為相容與短 URL 預設，文件提醒它不是機密 token；需要較強碰撞抵抗時使用 `sha256`。

## Migration Plan

這是新服務，沒有既有資料或 API migration。部署時準備 host share directory、設定 `.env`、執行 `docker compose up -d --build`，再以 `/healthz` 與首頁驗證；rollback 可停止 Compose service 並回到上一個 image，分享目錄不由服務寫入，因此不需資料復原。
