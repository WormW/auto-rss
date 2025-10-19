# 构建前端
FROM node:20-alpine AS web-builder
WORKDIR /build
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# 构建后端
FROM golang:1.21-alpine AS go-builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o auto-rss ./cmd/server

# 运行时环境
FROM alpine:latest
WORKDIR /app

# 安装运行时依赖
RUN apk --no-cache add ca-certificates sqlite-libs

# 复制构建产物
COPY --from=go-builder /build/auto-rss /app/
COPY --from=web-builder /build/dist /app/web/dist

# 创建数据目录
RUN mkdir -p /app/data

# 暴露端口
EXPOSE 7892

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:7892/health || exit 1

CMD ["/app/auto-rss"]
