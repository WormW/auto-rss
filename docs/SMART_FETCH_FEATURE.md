# 智能拉取策略功能文档

## 功能概述

基于番剧的更新日（AirDay）和本地库存状态，智能决定何时拉取 RSS，减少不必要的请求，提高资源利用效率。

## 核心特性

### 1. 时间窗口拉取

只在更新日前后拉取 RSS，避免平时无谓的请求：

```
更新日前1天 ──→ 更新日 ──→ 更新日后2天
    ↑                          ↑
 开始拉取                    停止拉取
```

默认配置：
- **更新日前1天**：开始高频拉取
- **更新日后2天**：继续拉取（可能还有v2版本、不同字幕组等）
- **其他时间**：低频检查或暂停拉取

### 2. 完结状态检查

| 状态 | 处理方式 |
|-----|---------|
| **连载中** | 按时间窗口策略拉取 |
| **已完结** | 降低拉取频率或跳过 |

判断是否完结：
- `CurrentEpisode >= TotalEpisodes` 且 `TotalEpisodes > 0`

### 3. 本地完整性检查

检查本地是否已下载完整：

```
总集数：24集
偏移：1集
已下载：1,2,3,5,6,7...（缺少第4集）
→ 即使不在时间窗口，也会拉取以补全
```

## 拉取决策矩阵

| 条件组合 | 是否应该拉取 | 拉取频率 | 说明 |
|---------|-------------|---------|------|
| 在窗口期 + 未完结 | ✅ 是 | 高频(10分钟) | 正常追番 |
| 不在窗口期 + 本地不完整 + 未完结 | ✅ 是 | 普通(30分钟) | 补全缺失 |
| 已完结 + 本地不完整 | ✅ 是 | 低频(60分钟) | 补全已完结 |
| 已完结 + 本地完整 | ❌ 否 | 极低频(24小时) | 偶尔检查v2 |
| 不在窗口期 + 本地完整 + 未完结 | ❌ 否 | 暂停至窗口期 | 等待更新 |

## 配置方式

### 配置项

```json
{
  "smart_fetch.enabled": true,           // 启用智能拉取
  "smart_fetch.before_air_day": 1,       // 更新日前1天开始
  "smart_fetch.after_air_day": 2,        // 更新日后2天结束
  "smart_fetch.skip_completed": false,   // 不跳过已完结（可能还有v2）
  "smart_fetch.check_local_complete": true // 检查本地完整性
}
```

### 订阅字段要求

订阅需要设置以下字段才能发挥智能拉取的最大效果：

```json
{
  "air_day": "5",           // 更新星期（0=周日，5=周五）
  "air_time": "23:00",      // 更新时间（可选）
  "total_episodes": 24,     // 总集数（用于判断是否完结）
  "current_episode": 12,    // 当前已下载集数
  "episode_offset": 0       // 集数偏移（可选）
}
```

## 日志输出

```
INFO Smart fetch evaluation completed
    total=15              // 总订阅数
    should_fetch=8        // 本次拉取数
    should_skip=7         // 跳过数
    in_window=3           // 在活跃窗口期的订阅
    completed=2           // 已完结的订阅

INFO Subscriptions with missing episodes
    list=["芙莉莲(2集)", "药屋少女(1集)"]

INFO Checking RSS feeds
    total=15
    will_fetch=8

DEBUG Skipping subscription based on smart fetch strategy
    subscription="某番剧"
    reason="waiting_for_window_3days"
    next_fetch_in=72h

INFO Parsed RSS feed
    subscription="芙莉莲"
    items=3
    fetch_reason="active_window_0days_until_air"
```

## 使用建议

### 最佳实践

1. **设置正确的更新日**
   - 使用 Mikan Project 或 Bangumi 查看番剧的更新时间
   - 更新日对智能拉取至关重要

2. **设置总集数**
   - 对于季番，设置 `total_episodes`（通常 12/13/24/25）
   - 对于年番或长期连载，保持默认 0（不判断完结）

3. **调整时间窗口**
   - 如果字幕组发布不规律，增大 `after_air_day`
   - 如果追求第一时间下载，增大 `before_air_day`

### 场景示例

**场景1：周五更新的番剧**
```
周一：不拉取（reason: waiting_for_window_4days）
周四：开始拉取（reason: active_window_1days_until_air，高频）
周五：继续拉取（reason: active_window_0days_until_air，高频）
周六：继续拉取（reason: active_window_1days_after，高频）
周日：继续拉取（reason: active_window_2days_after，高频）
周一：停止拉取（reason: waiting_for_window_4days）
```

**场景2：缺失集数补全**
```
芙莉莲：总24集，已下载22集，缺少第5、6集
→ 即使不在更新时间，也会继续拉取（reason: incomplete_missing_2_episodes）
```

**场景3：已完结补全**
```
某番剧：总12集，全部完结，本地只有10集
→ 低频拉取补全（reason: completed_but_incomplete_2_missing）
```

## 向后兼容

- 未设置 `air_day` 的订阅：默认随时拉取（保持原有行为）
- 未设置 `total_episodes` 的订阅：无法判断是否完结，默认当作连载中
- 可以单独禁用某个订阅的智能拉取（待实现）

## 注意事项

1. **首次启用**：需要一定时间收集下载数据来判断完整性
2. **字幕组延迟**：部分字幕组发布较晚，建议 `after_air_day` 设置至少2天
3. **特殊番剧**：半年番、年番建议不设置总集数或设置较大值
4. **v2版本**：已完结的番剧仍可能发布v2修正版，不建议完全跳过

## 未来扩展

- [ ] Web UI 智能拉取状态展示
- [ ] 单订阅禁用智能拉取开关
- [ ] 基于历史发布时间的自适应窗口
- [ ] 节假日/停播检测
- [ ] 多字幕组发布模式学习
