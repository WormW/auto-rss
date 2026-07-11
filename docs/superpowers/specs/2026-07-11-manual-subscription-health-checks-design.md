# 番剧健康诊断手动检查设计

## 背景

当前番剧健康诊断面板在打开时会一次性执行全部检查，包括访问 RSS、查询 qBittorrent、读取下载目录状态和磁盘空间。这使一个普通的查看动作产生外部网络请求、下载器请求和存储访问，也让用户无法控制检查范围。

现有实现还有四个需要同时修正的问题：

- RSS 健康检查没有应用 `system_proxy`，可能与实际 RSS 采集结果不一致。
- “缺失集数”实际只表示 `current_episode` 到 `latest_episode` 的进度差，并不证明磁盘上存在真实缺口。
- qBittorrent 检查会为每个活跃下载逐个请求任务信息，检查时间随活跃任务数增长。
- 批量重试失败或停滞任务会删除 qBittorrent payload，可能丢失已经下载的数据。

## 目标

- 打开健康诊断面板时不自动执行任何检查。
- 每个检查项必须由用户单独触发，且不提供“全部检查”按钮。
- 单项检查期间只显示该项的 loading 状态，其他项目仍可查看或触发。
- 汇总区域明确展示已经执行的检查数量，不把未检查项目误报为健康。
- 修复 RSS 代理、进度差命名、qBittorrent 批量查询和重试删除 payload 问题。
- 保留现有修复动作，但让依赖检查结果的动作在对应检查执行前保持禁用。

## 非目标

- 不引入后台定时番剧诊断。
- 不实现剧集台账或真实媒体文件完整性校验。
- 不新增“全部检查”或自动串行检查模式。
- 不改变手动扫描文件夹的深度扫描和应用流程。
- 不重构全局 `/health`、`/ready`、`/live` 系统健康端点。

## 接口设计

### 初始化诊断面板

保留：

```text
GET /api/v1/subscriptions/:id/diagnostics
```

该接口只读取订阅本身，返回检查项定义、全部 `unknown` 状态、检查进度和基础动作。它不得执行以下操作：

- 拉取 RSS。
- 查询 qBittorrent。
- 加载该订阅的全部下载记录。
- 对下载目录或磁盘执行 `os.Stat`、`Statfs`。

响应继续使用 `SubscriptionDiagnosticsResponse`，并新增：

```json
{
  "summary": {
    "overall": "unknown",
    "checked": 0,
    "total": 9,
    "healthy": 0,
    "warning": 0,
    "error": 0,
    "unknown": 9
  }
}
```

初始化时需要下载统计才能判断的动作保持禁用，原因显示为“请先检查下载任务”。

### 执行单项检查

新增：

```text
POST /api/v1/subscriptions/:id/diagnostics/checks/:key
```

支持固定检查键：

| key | 检查内容 | 允许的外部或存储访问 |
| --- | --- | --- |
| `subscription_enabled` | 订阅是否启用 | 无 |
| `rss_reachability` | RSS HTTP 状态、解析和响应时间 | RSS 网络请求 |
| `rss_freshness` | `last_check_time` 和 RSS 水位线 | 无 |
| `episode_progress` | 当前集数与最新集数之间的待收集范围 | 无 |
| `downloads` | 下载状态、失败项和可重试数量 | 下载记录查询 |
| `qbittorrent` | 下载器连接和活跃 hash 是否存在 | 下载记录查询、一次 qBittorrent 任务列表请求 |
| `files` | 已完成任务是否记录路径、预期目录是否存在 | 下载记录查询、预期目录 `Stat` |
| `organizer` | 重命名是否启用、完成任务是否记录重命名路径 | 下载记录查询 |
| `disk` | 下载根目录空间和阈值 | 根目录 `Stat`、`Statfs` |

未知 key 返回 HTTP 400。订阅不存在返回 HTTP 404。检查执行失败时返回该项的错误状态和可读原因，不触发其他检查。

单项响应包含：

```json
{
  "code": 0,
  "message": "Success",
  "data": {
    "check": {},
    "downloads": {},
    "files": {},
    "disk": {},
    "actions": []
  }
}
```

只有与该检查相关的扩展数据需要更新，前端将其合并到当前面板状态。

## 检查行为修正

### RSS 代理

`RSSHealthChecker` 增加代理设置能力。执行 `rss_reachability` 前读取 `system_proxy` 并应用到检查客户端。全局 RSS 健康检查 handler 同样使用该配置，保证手动采集和健康检查的网络路径一致。

无代理配置时必须显式清空已有代理，避免运行期间删除配置后继续复用旧代理。

### 待收集集数

将检查键从 `missing_episodes` 改为 `episode_progress`，界面标签从“缺失集数”改为“待收集集数”。计算仍使用订阅进度字段，但必须转换为季度内相对集数：

```text
relative_current = subscription.RelativeCurrentEpisode()
relative_latest = subscription.RelativeLatestEpisode()
pending = relative_current + 1 ... relative_latest
```

文案明确说明这是订阅进度差，不是磁盘文件扫描结果。真实文件缺口继续由“扫描本地文件”提供。

### qBittorrent 批量检查

执行 `qbittorrent` 检查时：

1. 查询该订阅下载记录并收集活动状态的 torrent hash。
2. 调用一次 `GetTorrentsByCategory("")` 获取当前 qBittorrent 任务。
3. 在内存中构建 hash 集合并计算缺失任务数量。

不得再为每个活动任务调用 `GetTorrentInfo`。

### 安全重试

批量重试失败或停滞任务时，如果旧 hash 存在，只调用 `RemoveTorrentTask`，保留 payload。随后重置数据库重试字段并重新添加种子。

删除旧任务失败时停止该条重试并返回失败，不得吞掉错误后继续创建新任务。这样可以避免旧任务仍存在时数据库状态与 qBittorrent 状态分叉。

## 前端交互

- 打开面板后显示九个检查项，状态均为“未检查”。
- 每项右侧显示“检查”按钮；已有结果时按钮显示“重新检查”。
- 每项拥有独立 loading 状态，不使用全局检查 loading 覆盖整个面板。
- 不显示“重新检查全部”或“全部检查”按钮。
- 汇总区显示“已检查 N/9”，并统计已执行项目的正常、警告和异常数量。
- 未检查项目保持灰色，不参与最坏状态计算；存在未检查项时整体文案显示“部分检查”或“尚未检查”。
- 单项成功后只合并该项结果及其相关指标，不清空其他项目结果。
- “重试失败任务”在 `downloads` 检查前禁用。
- “重新整理”和“重新命名”在 `downloads` 或相关文件检查前禁用。
- “扫描本地文件”“刷新 RSS”和“暂停/启用订阅”可根据订阅基础字段直接决定是否启用。

## 并发与错误处理

- 前端允许不同检查项并发执行，但同一检查项在运行时不能重复触发。
- 后端单项接口不共享可变的请求级状态，每次请求独立加载所需数据。
- RSS、qBittorrent 或磁盘检查失败只影响当前项目。
- 单项错误结果保留在卡片中，用户可直接重新检查。
- 修复动作完成后不自动执行任何健康检查；已有检查结果保持不变，并提示用户按需重新检查相关项目。

## 测试范围

### 后端

- 初始化接口不调用 RSS、qBittorrent、下载仓储或文件系统检查函数。
- 每个合法 key 只执行对应检查。
- 未知 key 返回 400。
- RSS 检查应用和清空 `system_proxy`。
- 待收集集数使用相对集数并输出新的键和文案。
- qBittorrent 检查只发起一次任务列表请求，不调用逐 hash 查询。
- 批量重试使用 `RemoveTorrentTask`，不调用 `DeleteTorrentWithPayload`。
- 删除旧任务失败时不重置数据库、不重新添加种子。

### 前端

- 打开面板后九项均为未检查，且不会调用单项检查接口。
- 点击一项只请求对应 key。
- 单项结果正确合并，其他检查结果不丢失。
- 没有“全部检查”按钮。
- 独立 loading 和重新检查文案正确。
- 汇总数量和最坏状态只根据已检查项目计算。

### 回归验证

- `go test ./internal/api/handler ./internal/service/rss ./internal/service/downloader -count=1`
- `go test ./... -count=1`
- 前端单元测试。
- `npm run build --prefix web`

## 兼容性

初始化接口路径保持不变，但不再返回即时检查结果。前端与后端必须在同一版本发布。旧客户端仍能解析原有字段，但会看到全部 `unknown`；新增 `checked`、`total` 和单项检查接口属于向后兼容扩展。
