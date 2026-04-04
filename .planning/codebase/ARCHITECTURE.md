# Architecture

**Analysis Date:** 2026-04-05

## Pattern Overview

**Overall:** Clean Architecture / Layered Architecture with Repository Pattern

**Key Characteristics:**
- Clear separation of concerns between API, business logic, and data layers
- Repository pattern for data access abstraction
- Service-oriented design for business logic
- Dependency injection via constructor functions
- Interface-based design for testability

## Layers

**API Layer (Handler):**
- Purpose: HTTP request handling, input validation, response formatting
- Location: `internal/api/handler/`
- Contains: Gin handlers, request/response DTOs
- Depends on: Service layer, Repository layer
- Used by: Router (`internal/api/router/router.go`)

**Router Layer:**
- Purpose: Route definitions, middleware orchestration, dependency wiring
- Location: `internal/api/router/router.go`
- Contains: Route groups, middleware stack, service initialization
- Depends on: Handlers, Services, Repositories
- Used by: Main entry point (`cmd/server/main.go`)

**Service Layer:**
- Purpose: Business logic, external API integrations, background tasks
- Location: `internal/service/`
- Contains: Business services (downloader, scheduler, bangumi, etc.)
- Depends on: Repositories, external APIs
- Used by: Handlers, other services

**Repository Layer:**
- Purpose: Data access abstraction, database operations
- Location: `internal/repository/`
- Contains: Repository interfaces and implementations
- Depends on: Models, GORM
- Used by: Services, Handlers

**Model Layer:**
- Purpose: Domain entities, database schema
- Location: `internal/model/`
- Contains: Struct definitions with GORM tags
- Depends on: None (pure data)
- Used by: All layers

**Configuration Layer:**
- Purpose: Application configuration management
- Location: `internal/config/`
- Contains: Config struct, loading logic, validation
- Depends on: Viper, environment variables
- Used by: All layers

**Package Layer (internal/pkg):**
- Purpose: Shared utilities, cross-cutting concerns
- Location: `internal/pkg/`
- Contains: Database, logger, constants, utilities
- Depends on: Configuration
- Used by: All layers

## Data Flow

**HTTP Request Flow:**

1. Request enters via Gin router (`internal/api/router/router.go`)
2. Middleware processes request (CORS, logging, recovery)
3. Handler receives request, validates input
4. Handler calls Service layer for business logic
5. Service calls Repository layer for data operations
6. Repository performs database operations via GORM
7. Response flows back up the stack

**Background Task Flow (RSS Scheduling):**

1. Scheduler (`internal/service/scheduler/scheduler.go`) triggers on cron schedule
2. Fetches active subscriptions from Repository
3. Parses RSS feeds via RSS Parser service
4. Filters items based on smart fetch strategy
5. Creates download records via Repository
6. Adds torrents to qBittorrent via downloader service
7. Updates subscription status

**Download Monitoring Flow:**

1. DownloadMonitor (`internal/service/downloader/monitor.go`) polls qBittorrent
2. Compares torrent states with database records
3. Updates download status in database
4. Triggers file renaming on completion
5. Sends notifications via notification service

## Key Abstractions

**Repository Interface:**
- Purpose: Abstract data access for testability
- Examples: `internal/repository/subscription.go`, `internal/repository/download.go`
- Pattern: Interface defines contract, struct implements with GORM

**Service Interface:**
- Purpose: Define business logic contracts
- Examples: `internal/service/downloader/qbittorrent.go` (QBittorrentClient interface)
- Pattern: Interface for external service clients

**App Context:**
- Purpose: Manage dynamically reloadable components
- Location: `internal/app/context.go`
- Pattern: Singleton with mutex-protected state

**Task Manager:**
- Purpose: Manage async background tasks with progress tracking
- Location: `internal/service/task/`
- Pattern: Singleton with channel-based communication

## Entry Points

**Main Server:**
- Location: `cmd/server/main.go`
- Triggers: Direct execution
- Responsibilities: 
  - Configuration loading
  - Database initialization and migration
  - Service initialization (qBittorrent, download monitor, Bangumi updater)
  - Router setup
  - Graceful shutdown handling

**WebSocket Endpoint:**
- Location: `internal/api/router/router.go` (route `/ws/notifications`)
- Triggers: Client WebSocket connection
- Responsibilities: Real-time notification delivery

**Static File Server:**
- Location: `internal/api/router/router.go`
- Triggers: HTTP requests to `/` or `/assets/*`
- Responsibilities: Serve built Vue.js frontend

## Error Handling

**Strategy:** Centralized error handling with structured logging

**Patterns:**
- Handlers return JSON error responses with consistent format: `{"code": N, "message": "..."}`
- Services log errors with context using structured logger (Zap)
- Repository errors bubble up to handlers
- Panic recovery via middleware (`internal/api/middleware/recovery.go`)

**Error Response Format:**
```go
gin.H{
    "code":    400-500,
    "message": "human readable error",
}
```

## Cross-Cutting Concerns

**Logging:** 
- Framework: Zap (Uber)
- Location: `internal/pkg/logger/`
- Pattern: Structured logging with key-value pairs
- Database logging: Logs written to both stdout and database

**Validation:**
- Framework: Gin binding with struct tags
- Pattern: `c.ShouldBindJSON(&request)` for input validation
- Custom validation in service layer for business rules

**Authentication:**
- Current: No authentication (single-user application)
- Note: Future consideration for multi-user support

**Transaction Management:**
- Pattern: GORM transactions in service layer
- Example: `s.db.Transaction(func(tx *gorm.DB) error { ... })`
- Used for: Atomic operations (download creation + status updates)

**Configuration Management:**
- Framework: Viper
- Sources: Environment variables, `.env` file, database (runtime config)
- Pattern: Load from env/file at startup, override from database

---

*Architecture analysis: 2026-04-05*
