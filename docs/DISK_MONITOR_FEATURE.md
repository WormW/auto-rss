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

- **正在观看保护**：可选保留最近有播放记录的番剧（需 Plex/Jellyfin 集成）
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
  "keep_days": 30
}

# 更新磁盘配置
PUT /api/v1/config
{
  "disk": {
    "warning_threshold_gb": 10,
    "auto_cleanup_enabled": true
  }
}
```

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
3. **保护机制有限**：未集成 Plex/Jellyfin 时，`protect_watching` 不会生效
4. **跨文件系统**：如果下载路径挂载在不同分区，监控的是该分区的空间

## 未来扩展

- [ ] Web UI 磁盘状态展示
- [ ] 磁盘使用趋势图表
- [ ] 手动清理界面
- [ ] Plex/Jellyfin 观看状态集成
- [ ] 多磁盘/分区监控
- [ ] 云存储（阿里云盘/OneDrive）空间监控
