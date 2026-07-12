# 字幕组功能设计文档

> **当前功能定位**: 字幕组名称是订阅 feed 的来源元数据；`subscriptions.subgroup_id` 是旧版 Mikan 兼容字段，不再代表整个订阅唯一的字幕组。
> **版本**: v0.1.0 (基础) → v0.3.0 (完整)
> **创建日期**: 2025-10-19

当前多 feed 行为、基线、水位线、先到先得和兼容策略以[多 feed 订阅设计](superpowers/specs/2026-07-11-multi-feed-subscription-design.md)为准。本文其余 v0.1.0/v0.3.0 内容保留为 `subgroup_id` 搜索与缓存的历史规划，不应据此把字幕组或集数偏移重新建模为订阅级唯一配置。

## 当前数据归属

| 信息 | 当前归属 | 用途 |
|------|----------|------|
| `subscription_feeds.fansub` | feed | 用户配置的来源字幕组；一个订阅可有多个 |
| 下载 `fansub` | 下载快照 | 保存实际资源解析出的字幕组 |
| 候选 `source_fansub`、`source_feed_name` | 候选快照 | feed 删除后仍可解释资源来源 |
| `subscriptions.fansub` | 兼容投影 | 仅供旧单 feed API 和旧客户端读取 |
| `subscriptions.subgroup_id` | 旧 Mikan 兼容字段 | 可保留站点 subgroup ID，但不控制其他 feeds |

新增或编辑来源时，字幕组名称与 RSS URL、集数偏移一起写入 feed。筛选、健康诊断和候选比较均应展示 feed 级来源；不能因为订阅兼容字段只保存第一条 feed，就假设整个订阅只有一个字幕组。

---

## 背景

Mikan Project (蜜柑计划) 为每个字幕组分配了唯一的 `subgroupid`，可用于：
- 精确筛选特定字幕组的番剧
- 构建字幕组专属 RSS 链接
- 提供更精准的订阅控制

### 常见使用场景

```
场景 1: 用户喜欢 "ANi" 字幕组的作品
- RSS标题: [ANi] 葬送的芙莉莲 - 12 [1080P][Baha][WEB-DL][AAC AVC][CHT]
- 提取字幕组名: "ANi"
- 匹配 subgroupid: 615 (Mikan数据库中的ID)
- 生成专属RSS: https://mikanani.me/RSS/Bangumi?bangumiId=3080&subgroupid=615

场景 2: 用户只想下载特定字幕组的高质量版本
- 通过 subgroupid 过滤，避免下载其他字幕组的低质量版本
```

---

## 功能分阶段设计

### v0.1.0 MVP: 基础字段预留

**实现内容**:
1. ✅ 数据库字段预留
   - `subscriptions.subgroup_id` (可选)
   - `downloads.fansub` (字幕组名称)

2. ✅ RSS 解析提取字幕组名称
   ```go
   // 从标题提取字幕组名称
   // [ANi] 葬送的芙莉莲 - 12 [1080P] → "ANi"
   // [LoliHouse] 药师少女的独语 / Kusuriya no Hitorigoto - 01 → "LoliHouse"

   func ExtractFansubName(title string) string {
       // 正则: ^\[([^\]]+)\]
       re := regexp.MustCompile(`^\[([^\]]+)\]`)
       matches := re.FindStringSubmatch(title)
       if len(matches) > 1 {
           return matches[1]
       }
       return ""
   }
   ```

3. ✅ Web UI 支持手动填写 subgroup_id
   - 订阅表单增加 "字幕组ID (可选)" 字段
   - 用户可手动填写从 Mikan 网站查到的 subgroupid

**不包含**:
- ❌ 字幕组搜索 API
- ❌ 自动匹配 subgroupid
- ❌ 字幕组数据库

---

### v0.3.0: 完整字幕组功能

**新增功能**:

#### 1. 字幕组搜索 API
```go
// internal/service/fansub/searcher.go
package fansub

type SubgroupInfo struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

// SearchSubgroups 搜索字幕组
// 通过爬取 Mikan 网站或调用第三方API获取字幕组列表
func SearchSubgroups(keyword string) ([]SubgroupInfo, error) {
    // 实现方式:
    // 1. 爬取 Mikan 字幕组列表页
    // 2. 或使用第三方 API (如果有)
    // 3. 模糊匹配关键词
    // 返回匹配的字幕组列表
}
```

#### 2. 自动匹配 subgroupid
```go
// internal/service/fansub/matcher.go
package fansub

// MatchSubgroupID 根据字幕组名称匹配ID
func MatchSubgroupID(fansubName string) (*int, error) {
    // 1. 从本地缓存查找 (优先)
    if id := getFromCache(fansubName); id != nil {
        return id, nil
    }

    // 2. 调用搜索API
    results, err := SearchSubgroups(fansubName)
    if err != nil {
        return nil, err
    }

    // 3. 精确匹配或相似度匹配
    for _, result := range results {
        if result.Name == fansubName {
            cacheSubgroup(fansubName, result.ID) // 缓存结果
            return &result.ID, nil
        }
    }

    // 4. 未找到精确匹配，返回 nil
    return nil, nil
}
```

#### 3. 字幕组数据缓存
```sql
-- 新增字幕组缓存表
CREATE TABLE fansub_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,           -- 字幕组名称
    subgroup_id INTEGER NOT NULL,        -- Mikan subgroupid
    last_updated DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_fansub_name ON fansub_cache(name);
CREATE INDEX idx_fansub_subgroup_id ON fansub_cache(subgroup_id);
```

#### 4. Web UI 增强
**订阅管理页**:
- 字幕组搜索框 (实时搜索)
- 自动填充 subgroup_id
- 显示匹配的字幕组列表供选择

**字幕组管理页** (新增):
- 已缓存的字幕组列表
- 手动添加/编辑字幕组
- 字幕组使用统计

---

## 数据模型详细设计

### subscriptions 表 (v0.1.0+)
```go
type Subscription struct {
    ID              uint      `json:"id" gorm:"primaryKey"`
    Name            string    `json:"name" gorm:"not null"`
    RssURL          string    `json:"rss_url" gorm:"not null"`
    Season          int       `json:"season" gorm:"default:1"`
    Status          string    `json:"status" gorm:"default:active"`
    FilterKeywords  string    `json:"filter_keywords" gorm:"type:text"`
    ExcludeKeywords string    `json:"exclude_keywords" gorm:"type:text"`
    SubgroupID      *int      `json:"subgroup_id"` // 👈 字幕组ID (可选)
    DownloadPath    string    `json:"download_path"`
    RenameEnabled   bool      `json:"rename_enabled" gorm:"default:true"`
    LastCheckTime   *time.Time `json:"last_check_time"`
    CreatedAt       time.Time `json:"created_at"`
    UpdatedAt       time.Time `json:"updated_at"`
}
```

**使用示例**:
```json
{
  "name": "葬送的芙莉莲",
  "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
  "season": 1,
  "subgroup_id": 615,  // ANi 字幕组的 ID
  "filter_keywords": ["1080p", "简体"]
}
```

### downloads 表 (v0.1.0+)
```go
type Download struct {
    ID             uint      `json:"id" gorm:"primaryKey"`
    SubscriptionID uint      `json:"subscription_id"`
    Title          string    `json:"title" gorm:"not null"`
    Episode        int       `json:"episode"`
    Fansub         string    `json:"fansub"` // 👈 字幕组名称
    TorrentURL     string    `json:"torrent_url" gorm:"not null"`
    TorrentHash    string    `json:"torrent_hash" gorm:"unique"`
    FilePath       string    `json:"file_path"`
    RenamedPath    string    `json:"renamed_path"`
    Status         string    `json:"status" gorm:"default:pending"`
    QbTaskID       string    `json:"qb_task_id"`
    ErrorMessage   string    `json:"error_message"`
    DownloadedAt   *time.Time `json:"downloaded_at"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

**使用示例**:
```json
{
  "subscription_id": 1,
  "title": "[ANi] 葬送的芙莉莲 - 12 [1080P][Baha][WEB-DL][AAC AVC][CHT]",
  "episode": 12,
  "fansub": "ANi",  // 从标题提取
  "torrent_url": "https://...",
  "status": "completed"
}
```

### fansub_cache 表 (v0.3.0+)
```go
type FansubCache struct {
    ID          uint      `json:"id" gorm:"primaryKey"`
    Name        string    `json:"name" gorm:"unique;not null"`
    SubgroupID  int       `json:"subgroup_id" gorm:"not null"`
    LastUpdated time.Time `json:"last_updated"`
}
```

---

## API 设计

### v0.1.0 MVP

#### 订阅创建 (支持 subgroup_id)
```
POST /api/v1/subscriptions

Request Body:
{
  "name": "葬送的芙莉莲",
  "rss_url": "https://mikanani.me/RSS/Bangumi?bangumiId=3080",
  "season": 1,
  "subgroup_id": 615,  // 可选，用户手动填写
  "filter_keywords": ["1080p", "简体"]
}

Response:
{
  "code": 0,
  "message": "订阅创建成功",
  "data": {
    "id": 1,
    "name": "葬送的芙莉莲",
    "subgroup_id": 615
    // ...
  }
}
```

### v0.3.0 完整功能

#### 1. 搜索字幕组
```
GET /api/v1/fansub/search?keyword=ANi

Response:
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": 615,
      "name": "ANi"
    },
    {
      "id": 382,
      "name": "ANi Raws"
    }
  ]
}
```

#### 2. 自动匹配字幕组 ID
```
POST /api/v1/fansub/match

Request Body:
{
  "fansub_name": "ANi"
}

Response:
{
  "code": 0,
  "message": "匹配成功",
  "data": {
    "fansub_name": "ANi",
    "subgroup_id": 615
  }
}
```

#### 3. 获取字幕组缓存列表
```
GET /api/v1/fansub/cache

Response:
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 50,
    "items": [
      {
        "id": 1,
        "name": "ANi",
        "subgroup_id": 615,
        "last_updated": "2025-10-19T12:00:00Z"
      },
      {
        "id": 2,
        "name": "LoliHouse",
        "subgroup_id": 382,
        "last_updated": "2025-10-18T10:00:00Z"
      }
    ]
  }
}
```

---

## 实现优先级

### v0.1.0 MVP (必须实现)
```
[x] 数据库字段添加 (subgroup_id, fansub)
[x] RSS解析提取字幕组名称
[x] 订阅表单支持手动填写 subgroup_id
[x] API 支持 subgroup_id 字段
```

### v0.3.0 (预留实现)
```
[ ] 字幕组搜索 API (爬取 Mikan 或第三方)
[ ] 自动匹配 subgroupid 逻辑
[ ] fansub_cache 表创建与管理
[ ] Web UI 字幕组搜索功能
[ ] 字幕组管理页
```

---

## 技术实现细节

### RSS 解析 - 字幕组名称提取

**标题格式分析**:
```
格式 1: [字幕组] 番剧名 - 集数 [其他信息]
- [ANi] 葬送的芙莉莲 - 12 [1080P][Baha][WEB-DL][AAC AVC][CHT]
- [LoliHouse] 药师少女的独语 - 01 [WebRip 1080p HEVC-10bit AAC]

格式 2: [字幕组][番剧名][集数][其他]
- [桜都字幕组][葬送的芙莉莲][12][1080p][简体]

格式 3: 无明确字幕组标识
- 葬送的芙莉莲 第12话 1080p
```

**提取逻辑**:
```go
// internal/service/rss/parser.go
package rss

import "regexp"

// ExtractFansubInfo 提取字幕组信息
func ExtractFansubInfo(title string) (fansubName string, hasFansub bool) {
    // 正则: 匹配标题开头的 [字幕组名]
    re := regexp.MustCompile(`^\[([^\]]+)\]`)
    matches := re.FindStringSubmatch(title)

    if len(matches) > 1 {
        return strings.TrimSpace(matches[1]), true
    }

    return "", false
}

// 使用示例
func ParseRSSItem(item *gofeed.Item) *model.Download {
    fansub, _ := ExtractFansubInfo(item.Title)

    return &model.Download{
        Title:  item.Title,
        Fansub: fansub, // 存储到数据库
        // ...
    }
}
```

### 字幕组 ID 手动配置 (v0.1.0)

**前端表单** (Vue 3):
```vue
<!-- web/src/views/Subscription.vue -->
<template>
  <n-form-item label="字幕组ID (可选)" path="subgroup_id">
    <n-input-number
      v-model:value="formData.subgroup_id"
      placeholder="可选，从 Mikan 网站查询"
      :min="1"
      clearable
    />
    <n-text depth="3" style="margin-left: 10px;">
      提示: 访问
      <a href="https://mikanani.me" target="_blank">Mikan Project</a>
      查看字幕组ID
    </n-text>
  </n-form-item>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const formData = ref({
  name: '',
  rss_url: '',
  season: 1,
  subgroup_id: null, // 可选
  // ...
})
</script>
```

### 字幕组搜索 (v0.3.0 预留)

**爬虫实现** (示例):
```go
// internal/service/fansub/crawler.go
package fansub

import (
    "fmt"
    "net/http"
    "github.com/PuerkitoBio/goquery"
)

type SubgroupInfo struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

// CrawlMikanSubgroups 爬取 Mikan 字幕组列表
func CrawlMikanSubgroups() ([]SubgroupInfo, error) {
    url := "https://mikanani.me/Home/ExpandSearchSubGroup"

    resp, err := http.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    doc, err := goquery.NewDocumentFromReader(resp.Body)
    if err != nil {
        return nil, err
    }

    var subgroups []SubgroupInfo

    // 解析页面提取字幕组ID和名称
    doc.Find(".subgroup-item").Each(func(i int, s *goquery.Selection) {
        idStr, _ := s.Attr("data-subgroupid")
        name := s.Text()

        var id int
        fmt.Sscanf(idStr, "%d", &id)

        subgroups = append(subgroups, SubgroupInfo{
            ID:   id,
            Name: name,
        })
    })

    return subgroups, nil
}

// SearchSubgroups 搜索字幕组
func SearchSubgroups(keyword string) ([]SubgroupInfo, error) {
    // 1. 获取全部字幕组列表 (带缓存)
    allSubgroups, err := getAllSubgroupsWithCache()
    if err != nil {
        return nil, err
    }

    // 2. 模糊匹配关键词
    var results []SubgroupInfo
    for _, sg := range allSubgroups {
        if strings.Contains(
            strings.ToLower(sg.Name),
            strings.ToLower(keyword),
        ) {
            results = append(results, sg)
        }
    }

    return results, nil
}
```

---

## 使用流程示例

### 流程 1: 手动配置字幕组 ID (v0.1.0)

```
1. 用户访问 Mikan Project 网站
2. 找到喜欢的字幕组，查看其 subgroupid
   - 例如: https://mikanani.me/Home/Bangumi/3080?subgroupid=615
   - 从URL提取 subgroupid = 615
3. 在 Auto-RSS 添加订阅时，手动填写 subgroup_id = 615
4. RSS URL 可直接使用带 subgroupid 的链接:
   - https://mikanani.me/RSS/Bangumi?bangumiId=3080&subgroupid=615
```

### 流程 2: 自动匹配字幕组 ID (v0.3.0)

```
1. Auto-RSS 解析RSS标题: [ANi] 葬送的芙莉莲 - 12 [1080P]...
2. 提取字幕组名称: "ANi"
3. 调用匹配服务: MatchSubgroupID("ANi")
4. 查询缓存或调用搜索API
5. 返回 subgroupid = 615
6. 自动更新订阅配置
7. 后续可使用精确的RSS链接
```

---

## 注意事项

### v0.1.0 实现注意点

1. **字段设计为可选**
   - `subgroup_id` 使用 `*int` (指针类型)，允许 NULL
   - 不影响现有功能，用户可选择性使用

2. **向后兼容**
   - 即使不填写 subgroup_id，系统依然正常工作
   - 仅提取和存储字幕组名称 (fansub)

3. **数据库索引**
   - 为 `subgroup_id` 和 `fansub` 创建索引
   - 优化查询性能

### v0.3.0 实现注意点

1. **数据源可靠性**
   - Mikan 网站结构可能变化，需要定期更新爬虫
   - 考虑缓存机制减少API调用

2. **搜索准确性**
   - 字幕组名称可能有变体 (ANi, ANi-Raws等)
   - 需要相似度匹配算法

3. **缓存策略**
   - 字幕组列表不常变化，可长期缓存
   - 定期更新缓存 (每周/每月)

---

## 总结

### v0.1.0 MVP
- ✅ 数据库支持字幕组相关字段
- ✅ RSS 解析提取字幕组名称
- ✅ Web UI 支持手动填写 subgroup_id
- ✅ 预留扩展接口

### v0.3.0 完整功能
- ⏳ 字幕组搜索 API
- ⏳ 自动匹配 subgroupid
- ⏳ 字幕组数据库缓存
- ⏳ Web UI 字幕组管理

**当前状态**: 所有必要的数据结构和接口已预留，v0.1.0 仅实现基础功能，v0.3.0 再扩展高级功能。

---

**文档版本**: v1.0
**最后更新**: 2025-10-19
**维护者**: Auto-RSS Team
