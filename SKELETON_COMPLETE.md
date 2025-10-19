# Auto-RSS 项目骨架生成完成

## 项目结构

已成功生成完整的项目骨架，结构如下:

```
auto-rss/
├── cmd/
│   └── server/
│       └── main.go                    # 应用入口
├── internal/
│   ├── api/
│   │   ├── handler/                   # API 处理器
│   │   │   ├── subscription.go
│   │   │   ├── download.go
│   │   │   ├── rss.go
│   │   │   └── config.go
│   │   ├── middleware/                # 中间件
│   │   │   ├── cors.go
│   │   │   ├── logger.go
│   │   │   └── recovery.go
│   │   └── router/
│   │       └── router.go              # 路由配置
│   ├── config/
│   │   └── config.go                  # 配置管理
│   ├── model/                         # 数据模型
│   │   ├── subscription.go
│   │   ├── download.go
│   │   ├── config.go
│   │   └── log.go
│   ├── repository/                    # 数据访问层
│   │   ├── subscription.go
│   │   ├── download.go
│   │   └── config.go
│   ├── service/                       # 业务逻辑层
│   │   ├── rss/
│   │   │   └── parser.go              # RSS 解析器 (TODO)
│   │   ├── downloader/
│   │   │   └── qbittorrent.go         # qBittorrent 客户端 (TODO)
│   │   ├── renamer/
│   │   │   └── renamer.go             # 文件重命名 (TODO)
│   │   └── scheduler/
│   │       └── scheduler.go           # 定时任务 (TODO)
│   └── pkg/                           # 公共包
│       ├── database/
│       │   └── database.go            # 数据库初始化
│       └── logger/
│           └── logger.go              # 日志封装
├── web/                               # 前端项目
│   ├── src/
│   │   ├── api/
│   │   │   └── index.ts               # API 客户端
│   │   ├── router/
│   │   │   └── index.ts               # 路由配置
│   │   ├── views/                     # 页面组件
│   │   │   ├── Subscriptions.vue
│   │   │   ├── Downloads.vue
│   │   │   ├── Config.vue
│   │   │   └── Logs.vue
│   │   ├── App.vue                    # 根组件
│   │   ├── main.ts                    # 应用入口
│   │   └── style.css                  # 全局样式
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   ├── tsconfig.node.json
│   └── vite.config.ts
├── docs/                              # 文档
│   ├── PRD.md                         # 产品需求文档
│   ├── API.md                         # API 文档
│   ├── DEPLOY.md                      # 部署文档
│   ├── FANSUB_DESIGN.md              # 字幕组功能设计
│   └── README.md                      # 项目说明
├── go.mod                             # Go 依赖管理
├── Dockerfile                         # Docker 镜像构建
├── docker-compose.yml                 # Docker Compose 配置
├── Makefile                           # 构建脚本
├── .gitignore                         # Git 忽略配置
└── .env.example                       # 环境变量示例
```

## 已完成的功能

### 后端 (Go)

#### 1. 核心基础设施 ✅
- [x] 项目结构和模块定义
- [x] 配置管理 (Viper + 环境变量)
- [x] 数据库初始化和迁移 (GORM + SQLite)
- [x] 结构化日志 (zap)
- [x] 应用入口和启动流程

#### 2. 数据层 ✅
- [x] 数据模型定义 (Subscription, Download, Config, Log)
- [x] 字幕组字段预留 (SubgroupID, Fansub)
- [x] Repository 接口和实现
  - SubscriptionRepository: CRUD + 列表查询
  - DownloadRepository: CRUD + 状态管理
  - ConfigRepository: 键值对存储

#### 3. API 层 ✅
- [x] RESTful API 路由
- [x] 中间件 (CORS, Logger, Recovery)
- [x] API 处理器
  - 订阅管理: 创建/更新/删除/查询
  - 下载管理: 查询/删除/重试
  - RSS 管理: 手动刷新 (TODO 实现)
  - 配置管理: 查询/更新
- [x] 统一响应格式
- [x] 静态文件服务 (前端)

#### 4. 服务层接口 ✅ (实现为 TODO)
- [x] RSS 解析器接口
  - FetchAndParse: RSS Feed 解析
  - ExtractFansub: 字幕组名称提取
  - ExtractEpisode: 集数提取
- [x] qBittorrent 客户端接口
  - Login, AddTorrent, GetTorrentInfo, DeleteTorrent
- [x] 文件重命名器接口
  - Rename, GenerateFileName (SxxExx 格式)
- [x] 定时任务调度器接口
  - Start, Stop, AddJob

### 前端 (Vue 3 + TypeScript)

#### 1. 项目基础 ✅
- [x] Vite + Vue 3 + TypeScript 配置
- [x] Vue Router 路由配置
- [x] Pinia 状态管理集成
- [x] Naive UI 组件库集成
- [x] Axios API 客户端

#### 2. 页面组件 ✅
- [x] App.vue: 主布局 + 侧边栏导航 + 主题切换
- [x] Subscriptions.vue: 订阅管理页面
  - 订阅列表展示
  - 添加订阅对话框
  - 删除订阅功能
- [x] Downloads.vue: 下载任务页面
  - 下载列表展示
  - 状态筛选
  - 重试/删除功能
- [x] Config.vue: 配置页面 (待实现)
- [x] Logs.vue: 日志页面 (待实现)

#### 3. API 集成 ✅
- [x] subscriptionApi: 订阅 CRUD
- [x] downloadApi: 下载查询/重试/删除
- [x] rssApi: RSS 手动刷新
- [x] configApi: 配置管理

### 部署和构建 ✅

#### 1. Docker 支持
- [x] 多阶段 Dockerfile
  - 前端构建阶段 (Node.js)
  - 后端构建阶段 (Go)
  - 运行时阶段 (Alpine)
- [x] docker-compose.yml 配置
- [x] 健康检查配置

#### 2. 构建工具
- [x] Makefile
  - 后端构建 (build, build-static)
  - 前端构建 (web-build)
  - 开发运行 (run, web-dev)
  - Docker 操作 (docker-build, docker-run)
  - 代码质量 (fmt, lint)

#### 3. 配置文件
- [x] .gitignore
- [x] .env.example

## 待实现功能 (TODO)

### v0.1.0 MVP (2-3 天)

1. **RSS 解析实现**
   - 使用 gofeed 解析 Mikan Project RSS
   - 提取字幕组名称 (正则: `^\[([^\]]+)\]`)
   - 提取集数信息

2. **qBittorrent 集成**
   - 实现登录认证
   - 添加种子任务
   - 监控下载状态

3. **定时任务**
   - RSS 定时检查 (默认 30 分钟)
   - 下载状态同步

4. **简单重命名**
   - SxxExx 格式生成
   - 文件移动到指定目录

### v0.2.0 增强功能 (3-5 天)
- TMDB/BGM 元数据采集
- 通知功能 (Telegram/邮件/Webhook)
- 智能季度检测

### v0.3.0 字幕组功能 (2-3 天)
- 字幕组搜索 API
- subgroupid 自动匹配
- 字幕组信息缓存

## 快速开始

### 1. 安装依赖

**后端**:
```bash
go mod download
```

**前端**:
```bash
cd web && npm install
```

### 2. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env 文件，配置 qBittorrent 连接信息
```

### 3. 运行开发环境

**后端**:
```bash
make run
# 或直接运行
go run cmd/server/main.go
```

**前端**:
```bash
make web-dev
# 或直接运行
cd web && npm run dev
```

### 4. 构建生产版本

```bash
# 构建前端
make web-build

# 构建后端
make build

# 使用 Docker
make docker-build
make docker-run
```

## 访问应用

- **前端**: http://localhost:5173 (开发模式) 或 http://localhost:7892 (生产模式)
- **API**: http://localhost:7892/api/v1
- **健康检查**: http://localhost:7892/health

## 下一步行动

1. **实现核心业务逻辑**
   - RSS 解析功能 ([internal/service/rss/parser.go](internal/service/rss/parser.go))
   - qBittorrent 客户端 ([internal/service/downloader/qbittorrent.go](internal/service/downloader/qbittorrent.go))
   - 文件重命名逻辑 ([internal/service/renamer/renamer.go](internal/service/renamer/renamer.go))
   - 定时任务调度 ([internal/service/scheduler/scheduler.go](internal/service/scheduler/scheduler.go))

2. **完善前端页面**
   - Config.vue 实现配置管理界面
   - Logs.vue 实现日志查询界面

3. **编写测试**
   - 单元测试
   - 集成测试
   - E2E 测试

4. **完善文档**
   - 用户手册
   - 开发者文档
   - API 使用示例

## 技术栈总结

**后端**:
- Go 1.21+
- Gin (Web 框架)
- GORM v2 (ORM)
- SQLite (数据库)
- gofeed (RSS 解析)
- zap (日志)
- robfig/cron (定时任务)
- go-resty (HTTP 客户端)

**前端**:
- Vue 3 (Composition API)
- TypeScript
- Vite (构建工具)
- Vue Router (路由)
- Pinia (状态管理)
- Naive UI (组件库)
- Axios (HTTP 客户端)

**部署**:
- Docker
- Docker Compose
- Makefile

---

**注意**: 所有标记为 TODO 的服务层实现都保留了接口和方法签名，可以按照 PRD 文档逐步实现。项目骨架已完全可编译和运行，只是核心业务逻辑需要后续填充。
