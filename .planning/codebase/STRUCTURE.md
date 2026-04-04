# Codebase Structure

**Analysis Date:** 2026-04-05

## Directory Layout

```
/Users/WormW/work/auto-rss/
├── cmd/
│   └── server/
│       └── main.go              # Application entry point
├── internal/
│   ├── api/
│   │   ├── handler/             # HTTP request handlers
│   │   ├── middleware/          # Gin middleware (CORS, logging, recovery)
│   │   └── router/
│   │       └── router.go        # Route definitions and wiring
│   ├── app/
│   │   └── context.go           # Application context for dynamic components
│   ├── config/
│   │   ├── config.go            # Configuration struct and loading
│   │   └── store.go             # Config storage operations
│   ├── model/
│   │   ├── subscription.go      # Subscription entity
│   │   ├── download.go          # Download task entity
│   │   ├── rss_source.go        # RSS source entity
│   │   ├── config.go            # Config entity
│   │   ├── log.go               # Log entry entity
│   │   └── notification.go      # Notification entities
│   ├── repository/
│   │   ├── subscription.go      # Subscription repository
│   │   ├── download.go          # Download repository
│   │   ├── rss_source_repository.go
│   │   ├── config.go            # Config repository
│   │   └── log.go               # Log repository
│   ├── service/
│   │   ├── bangumi/             # Bangumi API integration
│   │   ├── calendar/            # Calendar/scheduling service
│   │   ├── disk/                # Disk monitoring service
│   │   ├── downloader/          # qBittorrent integration
│   │   ├── mikan/               # Mikan RSS source integration
│   │   ├── notification/        # Notification service (WebSocket, Telegram, Webhook)
│   │   ├── organizer/           # File organization service
│   │   ├── renamer/             # File renaming service
│   │   ├── rss/                 # RSS parsing service
│   │   ├── scheduler/           # Background task scheduler
│   │   └── task/                # Async task management
│   ├── pkg/
│   │   ├── constants/           # Application constants
│   │   ├── database/            # Database connection and migrations
│   │   ├── logger/              # Structured logging setup
│   │   └── utils/               # Utility functions
│   └── webui/
│       ├── embed.go             # Embedded frontend (build tag: embed)
│       └── disk.go              # Disk-based frontend (build tag: !embed)
├── web/                         # Vue.js frontend
│   ├── src/
│   │   ├── api/                 # API client modules
│   │   ├── components/          # Vue components
│   │   ├── router/              # Vue Router configuration
│   │   ├── views/               # Page components
│   │   ├── App.vue              # Root component
│   │   ├── main.ts              # Frontend entry point
│   │   └── style.css            # Global styles
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── docs/                        # Documentation
├── claudedocs/                  # Claude-specific documentation
├── go.mod                       # Go module definition
├── go.sum                       # Go dependencies
├── Makefile                     # Build automation
├── Dockerfile                   # Container build
├── docker-compose.yml           # Container orchestration
└── .env.example                 # Environment template
```

## Directory Purposes

**cmd/server/:**
- Purpose: Application entry point
- Contains: `main.go` with initialization and lifecycle management
- Key files: `cmd/server/main.go`

**internal/api/handler/:**
- Purpose: HTTP request handlers
- Contains: 17 handler files for different domains
- Key files: `subscription.go` (64751 bytes - largest), `download.go`, `config.go`

**internal/api/middleware/:**
- Purpose: Cross-cutting HTTP concerns
- Contains: CORS, logging, panic recovery
- Key files: `cors.go`, `logger.go`, `recovery.go`

**internal/api/router/:**
- Purpose: Route definitions and dependency injection
- Contains: Single router setup file
- Key files: `router.go` (252 lines)

**internal/app/:**
- Purpose: Application-wide context management
- Contains: Dynamic component lifecycle (file organizer)
- Key files: `context.go`

**internal/config/:**
- Purpose: Configuration management
- Contains: Config struct, loading logic, database-backed config
- Key files: `config.go`, `store.go`

**internal/model/:**
- Purpose: Domain entities with GORM tags
- Contains: 6 model files
- Key files: `subscription.go` (73 lines), `download.go` (38 lines)

**internal/repository/:**
- Purpose: Data access layer
- Contains: Repository interfaces and implementations
- Key files: `subscription.go`, `download.go`, `config.go`
- Test files: `*_test.go` for status filtering and alignment

**internal/service/:**
- Purpose: Business logic and external integrations
- Subdirectories:
  - `bangumi/`: Bangumi API client
  - `calendar/`: Anime calendar service
  - `disk/`: Disk space monitoring
  - `downloader/`: qBittorrent integration (largest service)
  - `mikan/`: Mikan RSS scraping
  - `notification/`: Multi-channel notifications
  - `organizer/`: File organization
  - `renamer/`: File renaming templates
  - `rss/`: RSS feed parsing
  - `scheduler/`: Cron-based scheduling with smart fetch
  - `task/`: Async task management

**internal/pkg/:**
- Purpose: Shared utilities
- Subdirectories:
  - `constants/`: App constants
  - `database/`: SQLite/GORM setup
  - `logger/`: Zap logger configuration
  - `utils/`: Helper functions

**internal/webui/:**
- Purpose: Frontend serving abstraction
- Contains: Build tag-based filesystem abstraction
- Key files: `embed.go` (embedded), `disk.go` (development)

**web/src/:**
- Purpose: Vue.js 3 frontend
- Subdirectories:
  - `api/`: Axios-based API clients
  - `components/`: Reusable Vue components
  - `router/`: Vue Router setup
  - `views/`: Page-level components

## Key File Locations

**Entry Points:**
- Backend: `cmd/server/main.go`
- Frontend: `web/src/main.ts`

**Configuration:**
- Go mod: `go.mod`
- Env template: `.env.example`
- Docker: `Dockerfile`, `docker-compose.yml`

**Core Logic:**
- Router: `internal/api/router/router.go`
- Main handler: `internal/api/handler/subscription.go`
- Scheduler: `internal/service/scheduler/scheduler.go`
- qBittorrent client: `internal/service/downloader/qbittorrent.go`

**Testing:**
- Go tests: `*_test.go` files alongside source
- Frontend: No test files detected

## Naming Conventions

**Files:**
- Go: `snake_case.go` for implementation, `*_test.go` for tests
- Vue: `PascalCase.vue` for components
- TypeScript: `camelCase.ts` for modules

**Directories:**
- Go: `snake_case` or single word lowercase
- Vue: `camelCase` or single word lowercase

**Go Naming:**
- Interfaces: `ServiceName` (e.g., `SubscriptionRepository`)
- Implementations: `serviceName` or `serviceNameImpl` (unexported)
- Constructors: `NewServiceName`
- Handlers: `ServiceHandler` struct with handler methods

**Vue Naming:**
- Components: `PascalCase.vue`
- API modules: `camelCase.ts` with exported functions
- Views: `PascalCase.vue` matching route names

## Where to Add New Code

**New API Endpoint:**
- Handler: `internal/api/handler/{domain}.go`
- Route: `internal/api/router/router.go` (add to v1 group)
- API client: `web/src/api/{domain}.ts`

**New Domain/Feature:**
- Model: `internal/model/{domain}.go`
- Repository: `internal/repository/{domain}.go`
- Service: `internal/service/{domain}/{domain}.go`
- Handler: `internal/api/handler/{domain}.go`
- Route: Add to `internal/api/router/router.go`

**New Background Service:**
- Implementation: `internal/service/{name}/`
- Initialization: `cmd/server/main.go` or `internal/api/router/router.go`
- Lifecycle: Register with app context if dynamic

**New Frontend Page:**
- View: `web/src/views/{Name}.vue`
- Route: `web/src/router/index.ts`
- API: Add to `web/src/api/index.ts` or new file

**Utilities:**
- Shared helpers: `internal/pkg/utils/`
- Domain-specific: In service package

## Special Directories

**internal/webui/:**
- Purpose: Frontend serving abstraction with build tags
- Generated: `embed.go` requires built frontend in `web/dist/`
- Committed: Yes, but `web/dist/` is gitignored

**internal/pkg/database/migrations/:**
- Purpose: Database schema migrations
- Generated: No
- Committed: Yes

**data/:**
- Purpose: Runtime data (SQLite DB, covers)
- Generated: Yes (at runtime)
- Committed: No (in `.gitignore`)

**web/dist/:**
- Purpose: Built frontend assets
- Generated: Yes (`npm run build`)
- Committed: No (in `.gitignore`)

---

*Structure analysis: 2026-04-05*
