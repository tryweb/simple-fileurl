## Why

目前沒有一個可由 Docker Compose 啟動、將主機指定目錄安全地轉成可分享下載網址的服務。這項變更提供簡單、可部署於 Alpine 的檔案分享入口，讓使用者不必暴露原始檔案系統路徑即可分享檔案。

## What Changes

- 新增以 Docker Compose 啟動的 Alpine web service。
- 以 Docker Compose 將可配置的 Host share path 掛載到固定的 Container path，提供檔案下載而不暴露主機絕對路徑。
- 以可配置的 SFTP logical prefix 限制公開範圍，並將該 logical path 用於 hash URL 計算。
- 使用「目錄 hash / 檔案 hash」格式解析下載 URL。
- 支援 `file`（檔案內容 hash）與 `filename`（檔名 hash）兩種檔案識別模式。
- 支援 `md5`（預設）與 `sha256` hash 演算法。
- 提供基本檔案列表／分享網址產生介面，並使用設定的公開 URL 顯示連結。
- 對路徑穿越、非法路徑、越界 symlink 與非檔案目標進行拒絕。
- 提供健康檢查、錯誤狀態與 Docker Compose 設定範例。

## Capabilities

### New Capabilities

- `file-sharing`: 以可配置 hash URL 解析並安全下載分享根目錄中的檔案。

### Modified Capabilities

無。專案目前沒有既有 capability 規格。

## Impact

- 新增 web service、檔案系統解析與 hash 計算模組。
- 新增 HTTP 下載與基本檔案列表／連結產生介面。
- 新增 Dockerfile、Docker Compose 設定、環境變數文件與容器健康檢查。
- Compose 以 `HOST_SHARE_PATH` 指定 Host bind source，固定掛載至 Container `/opt/sharefiles`；服務以 `SHARE_PREFIX` 表示 SFTP 使用者看到的 logical path 前綴。
- 新增單元測試、HTTP 整合測試、路徑映射測試與容器啟動驗證。
- 服務採 bearer-link 模型：持有完整 URL 者即可下載；本變更不包含登入、權限管理、URL 到期或隨機 token。
