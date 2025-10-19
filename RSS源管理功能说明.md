# RSS 源管理功能实现说明

## 功能概述

实现了完整的 RSS 源管理系统，允许用户：
1. **管理 RSS 源**：添加、编辑、删除外部 RSS 源（如 Mikanani）
2. **浏览番剧列表**：从 RSS 源获取可订阅的番剧列表
3. **快速订阅**：从 RSS 源选择番剧一键创建订阅
4. **手动订阅**：也支持直接填写 RSS 地址创建订阅

## 数据模型

### RSSSource (RSS 源)
```go
type RSSSource struct {
    ID          uint      `json:"id"`
    Name        string    `json:"name"`        // RSS源名称，如"Mikanani"
    BaseURL     string    `json:"base_url"`    // RSS源基础URL
    Description string    `json:"description"` // 描述
    Enabled     bool      `json:"enabled"`     // 是否启用
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

### Subscription (订阅) - 新增字段
```go
type Subscription struct {
    // ... 原有字段 ...

    // 新增字段
    RSSSourceID *uint  `json:"rss_source_id"` // RSS源ID（如果从RSS源创建）
    SourceType  string `json:"source_type"`   // "manual": 手动填写, "rss_source": 从RSS源选择

    RSSSource   *RSSSource `json:"rss_source,omitempty"` // 关联的RSS源
}
```

### RSSAnime (RSS 番剧信息)
```go
type RSSAnime struct {
    Title      string   `json:"title"`       // 番剧标题
    RssURL     string   `json:"rss_url"`     // RSS订阅地址
    Fansub     string   `json:"fansub"`      // 字幕组
    UpdateDay  string   `json:"update_day"`  // 更新日期
    Episodes   []string `json:"episodes"`    // 已发布的集数
    SourceID   uint     `json:"source_id"`   // 来源RSS源ID
    SourceName string   `json:"source_name"` // 来源RSS源名称
}
```

## 后端 API

### RSS 源管理
- `POST /api/v1/rss-sources` - 创建 RSS 源
- `GET /api/v1/rss-sources` - 获取 RSS 源列表（支持分页和启用状态过滤）
- `GET /api/v1/rss-sources/:id` - 获取单个 RSS 源
- `PUT /api/v1/rss-sources/:id` - 更新 RSS 源
- `DELETE /api/v1/rss-sources/:id` - 删除 RSS 源
- `GET /api/v1/rss-sources/:id/animes` - 获取 RSS 源的番剧列表

### 请求示例

#### 创建 RSS 源
```json
POST /api/v1/rss-sources
{
  "name": "Mikanani",
  "base_url": "https://mikanani.me/RSS/Bangumi",
  "description": "蜜柑计划 RSS 源",
  "enabled": true
}
```

#### 获取番剧列表
```json
GET /api/v1/rss-sources/1/animes

Response:
{
  "data": [
    {
      "title": "某番剧名称",
      "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=xxx",
      "fansub": "某字幕组",
      "update_day": "周日",
      "episodes": ["01", "02", "03"],
      "source_id": 1,
      "source_name": "Mikanani"
    }
  ]
}
```

## 前端功能

### 1. RSS 源管理页面 (RSSSources.vue)

**位置**：菜单第一项 "RSS 源"

**功能**：
- 列表展示所有 RSS 源
- 添加新的 RSS 源（名称、URL、描述、启用状态）
- 删除 RSS 源（带确认对话框）
- 查看番剧列表（点击"查看番剧"按钮）

**番剧列表对话框**：
- 显示从 RSS 源解析的所有番剧
- 每个番剧显示：字幕组、标题、更新日期、集数
- 点击"订阅"按钮跳转到订阅页面并自动填充信息

### 2. 订阅管理页面 (Subscriptions.vue) - 增强

**新增功能**：
- 支持从 RSS 源页面跳转过来时自动填充表单
- 表单包含 `rss_source_id` 和 `source_type` 字段
- 区分手动创建和从 RSS 源创建的订阅

**创建流程**：

**方式1：从 RSS 源选择**
1. 进入 "RSS 源" 页面
2. 点击某个 RSS 源的 "查看番剧"
3. 浏览番剧列表
4. 点击想订阅的番剧的 "订阅" 按钮
5. 自动跳转到订阅页面，表单已自动填充：
   - 番剧名称
   - RSS 地址
   - RSS 源 ID
   - 来源类型设为 "rss_source"
6. 调整季数和下载路径，点击确定

**方式2：手动填写**
1. 直接进入 "订阅管理" 页面
2. 点击 "添加订阅"
3. 手动输入所有信息
4. 来源类型自动设为 "manual"

## 用户界面

### RSS 源列表页面布局
```
┌─────────────────────────────────────────────────────┐
│  RSS 源                            [添加 RSS 源]     │
├─────────────────────────────────────────────────────┤
│ ID │ 名称      │ RSS 地址      │ 描述  │ 状态 │ 操作 │
│ 1  │ Mikanani  │ https://...   │ ...   │ 启用 │ 查看番剧 删除 │
│ 2  │ 其他源    │ https://...   │ ...   │ 禁用 │ 查看番剧 删除 │
└─────────────────────────────────────────────────────┘
```

### 番剧列表对话框
```
┌─────────────────────────────────────────────────┐
│  Mikanani - 番剧列表                      [×]   │
├─────────────────────────────────────────────────┤
│ [某字幕组] 某番剧名称                            │
│            更新日期: 周日  集数: 01, 02, 03     │
│                                      [订阅]     │
├─────────────────────────────────────────────────┤
│ [另字幕组] 另一番剧名称                          │
│            更新日期: 周一  集数: 01             │
│                                      [订阅]     │
└─────────────────────────────────────────────────┘
```

## 技术实现细节

### 后端

1. **Repository 层**：`RSSSourceRepository`
   - 标准 CRUD 操作
   - 支持按启用状态过滤
   - 分页查询

2. **Handler 层**：`RSSSourceHandler`
   - 使用 `rss.Parser` 解析 RSS 源
   - 按番剧标题分组集数
   - 提取字幕组和集数信息

3. **番剧列表解析**：
   ```go
   // 从 RSS items 中提取番剧信息
   // 按标题分组，同一番剧的多集合并
   animeMap := make(map[string]*model.RSSAnime)
   for _, item := range items {
       // 提取字幕组和集数
       // 按标题分组
   }
   ```

### 前端

1. **API 客户端**：`rss-source.ts`
   - 完整的 TypeScript 类型定义
   - RESTful API 封装
   - 统一的错误处理

2. **路由配置**：
   - 新增 `/rss-sources` 路由
   - 默认首页改为 RSS 源页面
   - 菜单顺序：RSS 源 → 订阅管理 → 下载任务 → 系统配置 → 系统日志

3. **组件通信**：
   - 使用 `vue-router` 的 query 参数传递数据
   - RSS 源页面 → 订阅页面：传递番剧信息
   - 订阅页面检测 `from_rss` 参数自动填充表单

## 数据库迁移

新增 `rss_sources` 表：
```sql
CREATE TABLE rss_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(255) NOT NULL,
    base_url VARCHAR(255) NOT NULL,
    description TEXT,
    enabled BOOLEAN DEFAULT TRUE,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE INDEX idx_rss_sources_name ON rss_sources(name);
CREATE INDEX idx_rss_sources_enabled ON rss_sources(enabled);
```

`subscriptions` 表新增字段：
```sql
ALTER TABLE subscriptions ADD COLUMN rss_source_id INTEGER;
ALTER TABLE subscriptions ADD COLUMN source_type VARCHAR(20) DEFAULT 'manual';

CREATE INDEX idx_subscriptions_rss_source_id ON subscriptions(rss_source_id);
```

## 使用流程示例

### 场景：用户想追番「某新番」

1. **添加 RSS 源**（首次使用）
   - 进入"RSS 源"页面
   - 点击"添加 RSS 源"
   - 填写 Mikanani 信息
   - 保存

2. **浏览番剧**
   - 点击 Mikanani 的"查看番剧"
   - 浏览当季番剧列表

3. **订阅番剧**
   - 找到「某新番」
   - 点击"订阅"按钮
   - 自动跳转到订阅页面，信息已填充
   - 调整季数为 1，下载路径保持默认
   - 点击确定

4. **等待下载**
   - 系统自动检查 RSS 更新
   - 发现新集时自动下载
   - 下载完成后自动重命名

## 优势

### 用户体验
- **简化流程**：不需要手动复制 RSS 地址
- **可视化**：直观看到所有可订阅的番剧
- **信息丰富**：显示字幕组、更新日期、已发布集数
- **快速订阅**：一键订阅，无需手动填写

### 参考 ani-rss 的展示形式
- 类似的番剧列表展示
- 标签显示字幕组信息
- 列表式布局便于浏览
- 清晰的操作按钮

### 扩展性
- **多 RSS 源支持**：可以添加多个不同的 RSS 源
- **源管理**：可以启用/禁用特定源
- **追溯性**：知道订阅来自哪个 RSS 源
- **未来扩展**：可以基于 RSS 源做更多功能（如推荐、过滤等）

## 编译验证

### 后端编译
```bash
go build -o auto-rss cmd/server/main.go
# 成功生成 auto-rss 可执行文件
```

### 前端编译
```bash
cd web && npx vite build --mode production
# 成功生成 dist 目录
# 输出大小：773.43 kB (gzip: 226.18 kB)
```

## 文件清单

### 后端新增/修改文件
- `internal/model/rss_source.go` - RSS 源和番剧数据模型
- `internal/model/subscription.go` - 订阅模型新增字段
- `internal/repository/rss_source_repository.go` - RSS 源数据访问层
- `internal/api/handler/rss_source.go` - RSS 源 HTTP 处理器
- `internal/api/router/router.go` - 路由配置（新增 RSS 源路由）
- `internal/pkg/database/database.go` - 数据库迁移（新增 RSSSource）

### 前端新增/修改文件
- `web/src/api/rss-source.ts` - RSS 源 API 客户端
- `web/src/api/index.ts` - 导出 RSS 源 API
- `web/src/views/RSSSources.vue` - RSS 源管理页面（新建）
- `web/src/views/Subscriptions.vue` - 订阅管理页面（增强）
- `web/src/router/index.ts` - 路由配置（新增 RSS 源路由）
- `web/src/App.vue` - 主菜单（新增 RSS 源菜单项）

## 下一步建议

1. **缓存优化**：缓存番剧列表，避免频繁解析 RSS
2. **搜索功能**：在番剧列表中添加搜索/过滤
3. **推荐系统**：基于用户订阅历史推荐番剧
4. **批量订阅**：支持一次选择多个番剧订阅
5. **RSS 源模板**：预置常用 RSS 源（Mikanani、dmhy 等）
6. **更新通知**：RSS 源有新番剧时通知用户

## 总结

本次实现完成了从"直接填写 RSS 地址"到"从 RSS 源浏览选择番剧"的体验升级，参考了 ani-rss 的展示形式，使用户能够：
- 集中管理多个 RSS 源
- 浏览可订阅的番剧列表
- 快速一键订阅感兴趣的番剧
- 同时保留手动填写 RSS 地址的灵活性

整个功能从数据模型、后端 API、前端界面都已完整实现并编译通过。
