# 磁盘空间预警与自动清理功能文档

## 功能概述

监控磁盘空间使用情况，在空间不足时发送预警通知，并可配置自动清理策略释放空间。

## 核心特性

### 1. 三级空间状态

| 状态 | 条件 | 行为 |
|-----|------|------|
| 🟢 Healthy | 剩余空间 > 警告阈值 | 正常运行 |
| 🟡 Warning | 剩余空间 < 警告阈值 | 发送预警通知 |
| 🔴 Critical | 剩余空间 < 危险阈值 | 发送紧急通知 + 暂停下载 + 自动清理 |

默认阈值：
- 警告阈值：10 GB
- 危险阈值：5 GB

### 2. 三种清理策略

| 策略 | 说明 | 适用场景 |
|-----|------|---------|
| `age` | 删除 N 天前的已完成的下载 | 追新番用户 |
| `space` | 删除最旧的文件直到释放足够空间 | 小硬盘用户 |
| `hybrid` | 先按年龄清理，如仍不足则按空间清理 | 平衡方案 |

### 3. 保护机制

- **正在观看保护**：`disk.cleanup_protect_watching=true` 时，清理会查询 Jellyfin/Emby 或 Plex 的正在播放和最近播放路径，跳过仍在观看或最近播放的文件
- **保守失败策略**：保护开启但媒体库未配置、配置不完整或连接失败时，清理会跳过候选项，避免误删可能仍在使用的媒体文件
- **状态恢复**：空间恢复后自动恢复下载

## 配置方式

### 配置项

```json
{
  "disk.warning_threshold_gb": 10,      // 警告阈值(GB)
  "disk.critical_threshold_gb": 5,      // 危险阈值(GB)
  "disk.auto_cleanup_enabled": true,    // 自动清理开关
  "disk.cleanup_strategy": "hybrid",    // 清理策略
  "disk.cleanup_keep_days": 30,         // 保留天数
  "disk.cleanup_keep_gb": 50,           // 预留空间(GB)
  "disk.cleanup_protect_watching": true,// 保护正在观看的
  "disk.pause_on_critical": true        // 危险时暂停下载
}
```

### API 接口

```bash
# 获取磁盘状态
GET /api/v1/disk/status

# 获取磁盘信息
GET /api/v1/disk/info

# 手动触发清理
POST /api/v1/disk/cleanup
{
  "strategy": "age",
  "keep_days": 30,
  "keep_gb": 50
}

# 查询磁盘采样和清理历史
GET /api/v1/disk/history?page=1&page_size=20

# 更新磁盘配置
PUT /api/v1/disk/settings
{
  "enabled": true,
  "strategy": "hybrid",
  "retention_days": 30,
  "min_free_gb": 50,
  "warning_threshold_gb": 10,
  "critical_threshold_gb": 5,
  "protect_watching": true
}
```

手动清理和自动清理复用同一套清理逻辑，只会处理已完成下载记录。响应会返回 `deleted_count`、`skipped_count`、`failed_count`、`failed_paths`、`freed_bytes`、清理前后可用空间、`media_library_status` 和逐项 `items`。每次清理会持久化一条摘要，`/disk/history` 会返回磁盘采样 `samples`、分页清理摘要 `cleanup`、兼容别名 `list`、`total`、`page` 和 `page_size`。

## 工作流程

```
每5分钟检查磁盘空间
    ↓
获取磁盘使用情况
    ↓
判断状态（Healthy/Warning/Critical）
    ↓
状态变化？
    ├─ Warning → 发送预警通知
    ├─ Critical → 发送紧急通知 + 暂停下载
    └─ Healthy（从异常恢复）→ 发送恢复通知
    ↓
Critical + 自动清理启用？
    ├─ 是 → 执行清理策略
    └─ 否 → 结束
```

清理执行时会按策略筛选候选下载：

1. `age` 删除早于 `keep_days` 的已完成下载。
2. `space` 按最旧优先删除，直到当前可用空间加已释放空间达到 `keep_gb`。
3. `hybrid` 同时满足年龄清理和空间补足场景。

清理前会校验路径边界，拒绝删除下载根目录本身、空路径或下载根目录外的文件；这些项目会以 `skipped` 项和失败原因返回，并保留下载记录。

## 媒体库保护

启用 `disk.cleanup_protect_watching` 后，清理会读取以下配置：

```sql
'media_library.type'              -- jellyfin、emby 或 plex
'media_library.url'               -- 媒体库服务地址
'media_library.token'             -- API Token
'media_library.user_id'           -- Jellyfin/Emby 最近播放查询所需用户 ID，可选
'media_library.recent_play_hours' -- 最近播放保护窗口，默认 24 小时
```

- Jellyfin/Emby：查询 `/Sessions` 的 `NowPlayingItem.Path`；配置 `media_library.user_id` 后，还会查询最近播放条目的 `Path` 和 `DatePlayed`。
- Plex：查询 `/status/sessions` 和 `/status/sessions/history/all`，读取媒体分片 `file` 路径。
- 保护路径会转换为绝对路径并解析符号链接；候选下载的 `file_path` 和 `renamed_path` 与保护路径相同、互为父子路径时都会跳过。
- 保护开启但媒体库未配置、配置不完整、类型不支持或请求失败时，本次清理会保守跳过候选项，`media_library_status` 返回 `unconfigured` 或 `failed`。

## 通知事件

| 事件 | 触发条件 | 通知内容 |
|-----|---------|---------|
| `disk.warning` | 空间低于警告阈值 | 剩余空间提示，建议清理 |
| `disk.critical` | 空间低于危险阈值 | 紧急告警，下载已暂停 |
| `disk.recovered` | 空间恢复到健康状态 | 恢复通知，下载已恢复 |
| `disk.cleanup` | 自动清理完成 | 清理文件数、释放空间 |

## 日志输出

```
# 磁盘检查
DEBUG Disk check completed
    path=/downloads
    free_gb=45.23
    usage_percent=78.5%
    status=healthy

# 状态变化
WARN Disk space warning
    free_gb=8.50
    threshold_gb=10

ERROR Disk space critical
    free_gb=3.20
    threshold_gb=5

INFO Disk space recovered
    free_gb=12.80

# 自动清理
INFO Starting auto cleanup
    strategy=hybrid
    free_gb=3.20

INFO Auto cleanup completed
    cleaned_count=15
    cleaned_gb=23.50
```

## 数据库变更

### configs 表新增配置项

```sql
-- 磁盘监控配置
'disk.warning_threshold_gb'     -- 警告阈值
'disk.critical_threshold_gb'    -- 危险阈值
'disk.auto_cleanup_enabled'     -- 自动清理开关
'disk.cleanup_strategy'         -- 清理策略
'disk.cleanup_keep_days'        -- 保留天数
'disk.cleanup_keep_gb'          -- 预留空间
'disk.cleanup_protect_watching' -- 保护正在观看的
'disk.pause_on_critical'        -- 危险时暂停下载
```

## 向后兼容

- 所有配置有默认值，无需手动配置即可运行
- 自动清理默认关闭，需要手动启用
- 不影响现有功能，纯增量增强

## 注意事项

1. **文件删除不可逆**：自动清理会直接删除文件，请确保已开启保种或不需要原文件
2. **清理是异步的**：大文件删除可能需要时间，磁盘空间不会立即释放
3. **保护依赖媒体库路径一致性**：Plex/Jellyfin/Emby 返回的路径需要能与 Auto-RSS 记录的 `file_path` 或 `renamed_path` 对应；容器路径不一致时应配置一致的挂载或路径映射
4. **跨文件系统**：如果下载路径挂载在不同分区，监控的是该分区的空间

## 未来扩展

- [ ] 多磁盘/分区监控
- [ ] 云存储（阿里云盘/OneDrive）空间监控
