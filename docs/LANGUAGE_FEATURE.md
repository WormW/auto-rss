# 简繁智能处置功能文档

## 功能概述

当 RSS 源中同时包含简体中文和繁体中文版本时，本功能可以智能选择只下载用户偏好的语言版本，避免同一集下载两份（12集变成24个文件的问题）。

## 核心特性

### 1. 自动语言识别
- 支持识别多种语言标签格式：
  - **简体中文**: `CHS`, `SC`, `简体`, `简中`, `GB`, `简`
  - **繁体中文**: `CHT`, `TC`, `繁体`, `繁中`, `Big5`, `繁`
  - **日语**: `JPN`, `JP`, `日本語`, `RAW`, `生肉`
  - **英语**: `ENG`, `EN`, `English`

### 2. 四种语言偏好策略

| 策略 | 说明 | 适用场景 |
|-----|------|---------|
| `auto` | 自动学习：根据历史下载记录推断偏好 | 默认推荐 |
| `chs` | 简体中文优先：优先简体，无简体时下载繁体 | 大陆用户 |
| `cht` | 繁体中文优先：优先繁体，无繁体时下载简体 | 港澳台用户 |
| `both` | 同时保留：简体和繁体都下载 | 特殊需求 |

### 3. 自动学习算法
- 分析最近 20 条下载记录
- 如果某语言占比 > 60%，自动推断为该语言优先
- 数据不足或比例均衡时，默认简体优先

### 4. 智能去重
- 同一集同一语言：只保留最新版本（V2替换V1）
- 同一集不同语言：根据策略决定是否跳过

## 配置方式

### 订阅级别配置

每个订阅可以独立设置语言偏好：

```json
{
  "name": "葬送的芙莉莲",
  "rss_url": "https://mikanani.me/RSS/...",
  "language_preference": "chs"
}
```

### API 端点

```bash
# 创建订阅时指定语言偏好
POST /api/v1/subscriptions
{
  "name": "芙莉莲",
  "rss_url": "...",
  "language_preference": "chs"  // auto/chs/cht/both
}

# 更新订阅的语言偏好
PUT /api/v1/subscriptions/:id
{
  "language_preference": "cht"
}
```

## 处理示例

### 场景 1：CHS 优先策略
```
RSS 条目按发布时间排序：
1. [字幕组] 芙莉莲 - 12 [1080P][CHS]      → 检测到 CHS，下载
2. [字幕组] 芙莉莲 - 12 [1080P][CHT]      → 检测到 CHT，已有 CHS，跳过
3. [字幕组] 芙莉莲 - 12 [1080P][CHS][v2]  → 检测到 CHS，版本更新，替换 #1

最终结果：只下载了 v2 简体版本
```

### 场景 2：Auto 自动学习
```
用户历史下载记录：15个简体，2个繁体
推断偏好：简体优先（>60%）

新番剧 RSS：
1. [字幕组] 新番 - 01 [CHT]  → 检测到 CHT，无 CHS，下载（降级）
2. [字幕组] 新番 - 01 [CHS]  → 检测到 CHS，优先语言，下载
3. [字幕组] 新番 - 01 [CHT][v2]  → 已有 CHS，跳过

最终结果：下载了简体 + 第一个繁体（当无简体时）
```

### 场景 3：Both 双语言
```
RSS 条目：
1. [字幕组A] 芙莉莲 - 12 [CHS]  → 下载
2. [字幕组B] 芙莉莲 - 12 [CHT]  → 下载（不同语言）
3. [字幕组C] 芙莉莲 - 12 [CHS]  → 跳过（同语言已有）

最终结果：简体和繁体各保留一个
```

## 数据库变更

### subscriptions 表新增字段
```sql
language_preference VARCHAR(10) DEFAULT 'auto'
```

### downloads 表新增字段
```sql
language VARCHAR(10) DEFAULT 'unknown'
```

创建复合索引优化查询：
```sql
CREATE INDEX idx_downloads_sub_ep_lang ON downloads(subscription_id, episode, language);
```

## 日志输出

启用语言过滤后，日志会显示决策过程：

```
# 成功下载
INFO Download task created
    language=chs
    lang_keyword=CHS

# 跳过下载
DEBUG Skipping download based on language policy
    episode=12
    language=cht
    reason=chs_exists_skip_cht

# 版本替换
INFO Found newer version, replacing old download
    episode=12
    language=chs
    old_title=...v1...
    new_title=...v2...
```

## 向后兼容

- 未设置 `language_preference` 的订阅默认使用 `auto` 模式
- 已有下载记录的语言字段会自动推断回填
- 不影响现有功能，纯增量增强

## 注意事项

1. **混合标签处理**：`[简日]`、`[繁日]` 等混合标签优先识别为中文
2. **未知语言**：无法识别的语言会下载（保守策略，避免漏掉）
3. **版本号识别**：支持 `v2`、`V3` 等版本标识，高版本会自动替换低版本
4. **时序依赖**：按 RSS 发布时间顺序处理，后发布的条目可能替换先下载的

## 未来扩展

- [ ] Web UI 语言偏好可视化设置
- [ ] 按字幕组设置语言偏好
- [ ] 分辨率优先级与语言优先级组合策略
- [ ] 用户全局默认语言偏好设置
