.PHONY: all build build-embed clean run test docker-build docker-run web-install web-build web-dev web-embed install-service uninstall-service

GOCACHE_DIR ?= $(CURDIR)/.cache/go-build
GOMODCACHE_DIR ?= $(CURDIR)/.cache/go-mod

# 构建产物名称
BINARY_NAME=auto-rss

# 默认目标
all: build

# 构建后端
build:
	@echo "Building backend..."
	@GOCACHE=$(GOCACHE_DIR) GOMODCACHE=$(GOMODCACHE_DIR) go build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/server

# 构建后端 (嵌入前端资源)
build-embed: web-embed
	@echo "Building backend with embedded frontend..."
	@GOCACHE=$(GOCACHE_DIR) GOMODCACHE=$(GOMODCACHE_DIR) go build -tags embed -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/server

# 构建用于生产的静态链接二进制
build-static:
	@echo "Building static binary..."
	@GOCACHE=$(GOCACHE_DIR) GOMODCACHE=$(GOMODCACHE_DIR) CGO_ENABLED=1 go build -ldflags="-s -w -extldflags '-static'" -o $(BINARY_NAME) ./cmd/server

# 清理构建产物
clean:
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)
	@rm -rf bin/
	@rm -rf web/dist/
	@rm -rf internal/webui/dist/
	@rm -rf data/

# 运行后端
run: build
	@echo "Running backend..."
	@./$(BINARY_NAME)

# 运行测试
test:
	@echo "Running tests..."
	@go test -v ./...

# 安装前端依赖
web-install:
	@echo "Installing frontend dependencies..."
	@cd web && npm install

# 构建前端
web-build: web-install
	@echo "Building frontend..."
	@cd web && npm run build

# 准备嵌入的前端资源
web-embed: web-build
	@echo "Preparing embedded frontend..."
	@rm -rf internal/webui/dist/
	@mkdir -p internal/webui/dist
	@cp -R web/dist/* internal/webui/dist/

# 运行前端开发服务器
web-dev: web-install
	@echo "Starting frontend dev server..."
	@cd web && npm run dev

# 构建 Docker 镜像
docker-build:
	@echo "Building Docker image..."
	@docker build -t wormw/auto-rss:latest .

# 运行 Docker 容器
docker-run: docker-build
	@echo "Running Docker container..."
	@docker-compose up -d

# 停止 Docker 容器
docker-stop:
	@echo "Stopping Docker container..."
	@docker-compose down

# 格式化代码
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# 代码检查
lint:
	@echo "Linting code..."
	@golangci-lint run

# 更新依赖
deps:
	@echo "Updating dependencies..."
	@go mod tidy
	@cd web && npm update

# 服务管理 (macOS/Linux)
install-service:
	@echo "Installing system service..."
	@./scripts/install-service.sh

uninstall-service:
	@echo "Uninstalling system service..."
	@./scripts/uninstall-service.sh

# 帮助信息
help:
	@echo "Available targets:"
	@echo "  make build             - Build backend (output: ./auto-rss)"
	@echo "  make build-embed       - Build backend with embedded frontend"
	@echo "  make build-static      - Build static binary"
	@echo "  make clean             - Clean build artifacts"
	@echo "  make run               - Run backend"
	@echo "  make test              - Run tests"
	@echo "  make web-install       - Install frontend dependencies"
	@echo "  make web-build         - Build frontend"
	@echo "  make web-embed         - Prepare embedded frontend assets"
	@echo "  make web-dev           - Run frontend dev server"
	@echo "  make docker-build      - Build Docker image"
	@echo "  make docker-run        - Run Docker container"
	@echo "  make docker-stop       - Stop Docker container"
	@echo "  make install-service   - Install as system service"
	@echo "  make uninstall-service - Uninstall system service"
	@echo "  make fmt               - Format code"
	@echo "  make lint              - Lint code"
	@echo "  make deps              - Update dependencies"
