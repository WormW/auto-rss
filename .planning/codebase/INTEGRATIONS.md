# External Integrations

**Analysis Date:** 2026-04-05

## APIs & External Services

**qBittorrent Web API:**
- Purpose: Download management - adding torrents, monitoring progress, file operations
- Implementation: REST API client in `/Users/WormW/work/auto-rss/internal/service/downloader/qbittorrent.go`
- Authentication: Cookie-based (SID) via `/api/v2/auth/login`
- Key operations:
  - `AddTorrent` - Add torrent via URL or magnet link
  - `AddTorrentFile` - Add torrent via file upload
  - `GetTorrentInfo` - Get download progress and state
  - `SetCategory` - Organize torrents by category
  - `RenameTorrentFile` - Rename downloaded files
  - `DeleteTorrent` - Remove torrents
- Configuration: `QB_HOST`, `QB_USERNAME`, `QB_PASSWORD` env vars

**Mikan Project (蜜柑计划):**
- Purpose: Anime RSS source and metadata scraping
- Implementation: HTML scraper in `/Users/WormW/work/auto-rss/internal/service/mikan/mikan.go`
- Base URL: `https://mikanime.tv` (configurable)
- Operations:
  - `Search` - Search anime by keyword
  - `GetBySeason` - Browse by year/season
  - `GetFansubGroups` - Get subtitle groups for an anime
- Features: Proxy support for network access

**Bangumi API (bgm.tv):**
- Purpose: Anime metadata, ratings, episode information
- Implementation: REST client in `/Users/WormW/work/auto-rss/internal/service/bangumi/bangumi.go`
- Base URL: `https://api.bgm.tv`
- API Version: v0
- Operations:
  - `Search` - Search subjects by keyword
  - `GetSubject` - Get detailed anime information
  - `GetSubjectEpisodes` - Get episode list
  - `SearchByName` - Auto-match best result
- Features: Proxy support, Chinese date parsing, season extraction

**Telegram Bot API:**
- Purpose: Push notifications
- Implementation: `/Users/WormW/work/auto-rss/internal/service/notification/telegram.go`
- Base URL: `https://api.telegram.org/bot{token}/{method}`
- Authentication: Bot token + Chat ID
- Operations: `SendMessage` with MarkdownV2 formatting
- Configuration: Stored in database via notification settings

**Generic Webhook:**
- Purpose: Custom notification endpoints
- Implementation: `/Users/WormW/work/auto-rss/internal/service/notification/webhook.go`
- Features:
  - Custom HTTP methods (POST/PUT/PATCH)
  - Custom headers
  - Body templates (Go template syntax)
  - HMAC-SHA256 signature support
  - Retry logic (3 attempts with backoff)
  - Predefined templates: default, nanobot, openclaw, discord, slack

## Data Storage

**Databases:**
- SQLite 3 - Primary data store
  - Driver: `gorm.io/driver/sqlite` v1.5.5
  - ORM: GORM v1.25.7
  - Connection: File-based (`DB_PATH` env var)
  - Location: Configurable (default: `./data/auto-rss.db`)
  - Models: `/Users/WormW/work/auto-rss/internal/model/`
    - `Subscription` - Anime subscriptions
    - `Download` - Download records
    - `RSSSource` - RSS source definitions
    - `Config` - Application configuration
    - `Log` - Structured log entries
    - `Notification` - Notification history
    - `NotificationSetting` - Channel configurations

**File Storage:**
- Local filesystem only
- Key directories:
  - `data/covers/` - Downloaded anime cover images
  - `downloads/` - Torrent download destination
  - `data/` - SQLite database

**Caching:**
- No external cache service
- In-memory caching via Go data structures
- File system watcher for config reload (`fsnotify`)

## Authentication & Identity

**Auth Provider:**
- No external auth provider
- qBittorrent authentication handled separately
- Application has no user management - single-user design

**API Security:**
- CORS enabled for Web UI (`/Users/WormW/work/auto-rss/internal/api/middleware/cors.go`)
- No API key or JWT authentication currently implemented

## Monitoring & Observability

**Error Tracking:**
- Structured logging with Zap (`/Users/WormW/work/auto-rss/internal/pkg/logger/logger.go`)
- Log levels: debug, info, warn, error
- Dual output: Console (stderr) + Database
- Log viewer API in `/Users/WormW/work/auto-rss/internal/api/handler/log.go`

**Health Checks:**
- HTTP endpoint: `GET /health`
- Docker healthcheck: `wget --spider http://localhost:7892/health`

**WebSocket:**
- Real-time notifications via WebSocket
- Endpoint: `/ws/notifications`
- Implementation: `/Users/WormW/work/auto-rss/internal/service/notification/websocket.go`

## CI/CD & Deployment

**Hosting:**
- Docker container deployment
- Image: `wormw/auto-rss:latest`
- Multi-stage Dockerfile (Node + Go + Alpine)

**CI Pipeline:**
- Not detected in repository

**Build Process:**
1. Frontend build: `npm run build` (produces `web/dist/`)
2. Backend build: `go build` with optional embed tag
3. Docker multi-stage build combining both

## Environment Configuration

**Required env vars:**
- `DB_PATH` - SQLite database file path
- `QB_HOST` - qBittorrent Web UI URL
- `QB_USERNAME` - qBittorrent username
- `QB_PASSWORD` - qBittorrent password

**Optional env vars:**
- `RSS_INTERVAL` - RSS check frequency (default: `30m`)
- `LOG_LEVEL` - Log verbosity (default: `info`)
- `SERVER_PORT` - HTTP port (default: `7892`)
- `DOWNLOAD_PATH` - Download directory (default: `/downloads`)
- `BANGUMI_UPDATE_INTERVAL` - Bangumi metadata update interval in hours (default: `6`)
- `FILE_ORGANIZER_ENABLED` - Enable file auto-organization (default: `false`)
- `FILE_ORGANIZER_DIR` - File organizer directory

**Secrets location:**
- Environment variables or `.env` file
- Database-stored configs for qBittorrent credentials
- Notification settings stored in database

## Webhooks & Callbacks

**Incoming:**
- None

**Outgoing:**
- Telegram Bot API - Notification messages
- Configurable Webhooks - Custom notification endpoints
  - Events: download complete, RSS update, errors
  - Configurable via `/api/v1/notifications/settings` API

## Network Architecture

**Ports:**
- `7892` - Main HTTP server port
- `5173` - Vite dev server (development only)

**Proxy Support:**
- HTTP proxy support for:
  - Mikan Project scraping
  - Bangumi API calls
  - RSS feed fetching
  - Telegram API
  - Webhook notifications
  - Torrent file downloads
- Configured per-service via settings API

---

*Integration audit: 2026-04-05*
