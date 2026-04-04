# Technology Stack

**Analysis Date:** 2026-04-05

## Languages

**Primary:**
- Go 1.23.0 - Backend API and services (`/Users/WormW/work/auto-rss/go.mod`)
- TypeScript - Frontend Vue 3 application (`/Users/WormW/work/auto-rss/web/tsconfig.json`)

**Secondary:**
- HTML/CSS - Vue SFC templates and styling
- SQL - SQLite database operations via GORM

## Runtime

**Environment:**
- Go 1.23.0 (alpine-based Docker images)
- Node.js 20 (for frontend build)

**Package Manager:**
- Go Modules (`go.mod`, `go.sum`)
- npm (`web/package.json`, `web/package-lock.json`)
- Lockfile: Present for both Go and npm

## Frameworks

**Core Backend:**
- Gin v1.10.0 (`/Users/WormW/work/auto-rss/go.mod`) - HTTP web framework
- GORM v1.25.7 (`/Users/WormW/work/auto-rss/go.mod`) - ORM for database operations

**Frontend:**
- Vue 3.4.0 (`/Users/WormW/work/auto-rss/web/package.json`) - Progressive JavaScript framework
- Vue Router 4.2.0 - Client-side routing
- Pinia 2.1.0 - State management
- Naive UI 2.38.0 - Component library

**Testing:**
- testify v1.9.0 - Go testing framework
- Go's built-in `testing` package

**Build/Dev:**
- Vite 5.0.0 - Frontend build tool and dev server
- vue-tsc - TypeScript compiler for Vue
- Make - Build orchestration (`/Users/WormW/work/auto-rss/Makefile`)

## Key Dependencies

**Critical Backend:**
- `github.com/gin-gonic/gin` v1.10.0 - Web framework
- `gorm.io/gorm` v1.25.7 - ORM
- `gorm.io/driver/sqlite` v1.5.5 - SQLite driver
- `github.com/mmcdole/gofeed` v1.3.0 - RSS feed parsing
- `github.com/PuerkitoBio/goquery` v1.10.3 - HTML parsing (Mikan scraping)
- `github.com/robfig/cron/v3` v3.0.1 - Cron scheduling
- `github.com/spf13/viper` v1.18.2 - Configuration management
- `go.uber.org/zap` v1.27.0 - Structured logging
- `github.com/gorilla/websocket` v1.5.3 - WebSocket support
- `github.com/go-resty/resty/v2` v2.16.5 - HTTP client for API calls
- `github.com/fsnotify/fsnotify` v1.9.0 - File system notifications

**Critical Frontend:**
- `vue` ^3.4.0 - Core framework
- `naive-ui` ^2.38.0 - UI component library
- `pinia` ^2.1.0 - State management
- `vue-router` ^4.2.0 - Routing
- `axios` ^1.6.0 - HTTP client
- `@vicons/antd` & `@vicons/ionicons5` - Icon libraries

**Infrastructure:**
- SQLite 3 - Embedded database
- Docker & Docker Compose - Containerization

## Configuration

**Environment:**
- Configuration via environment variables OR `.env` file
- Viper for configuration management (`/Users/WormW/work/auto-rss/internal/config/config.go`)
- Key config files:
  - `.env.example` - Example configuration template
  - `docker-compose.yml` - Docker deployment config
  - `Dockerfile` - Multi-stage build definition

**Build:**
- `vite.config.ts` - Vite configuration with proxy to backend
- `tsconfig.json` - TypeScript compiler configuration
- `Makefile` - Build targets for dev and production

**Required Environment Variables:**
- `DB_PATH` - SQLite database path (default: `./data/auto-rss.db`)
- `QB_HOST` - qBittorrent Web UI URL
- `QB_USERNAME` - qBittorrent username
- `QB_PASSWORD` - qBittorrent password
- `RSS_INTERVAL` - RSS check interval (default: `30m`)
- `LOG_LEVEL` - Logging level (default: `info`)
- `SERVER_PORT` - HTTP server port (default: `7892`)
- `DOWNLOAD_PATH` - Download directory path

## Platform Requirements

**Development:**
- Go 1.23+
- Node.js 18+
- SQLite 3.40+
- Make

**Production:**
- Docker 20.10+ (recommended)
- Or Linux/macOS/Windows binary
- qBittorrent 3.0+ with Web UI enabled (external dependency)

**Build Targets:**
- `make build` - Build backend binary
- `make build-embed` - Build with embedded frontend
- `make web-dev` - Run frontend dev server (port 5173)
- `make docker-build` - Build Docker image
- `make test` - Run Go tests

---

*Stack analysis: 2026-04-05*
