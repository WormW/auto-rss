# Auto-RSS 扫描恢复计划

> **CRITICAL:** 本计划必须在获得用户明确批准后方可实施。在批准之前，不会写入任何代码或修改数据库。

---

## 1. 现状分析：剧集如何被保存和命名

### 1.1 下载路径

- **配置来源**：数据库 `configs` 表，`key = "download_path"`，实际值为 `/Volumes/仓库/Bangumi`。
- **默认路径**：`/downloads`（由 `internal/config/config.go` 和 `internal/pkg/constants/app.go` 定义）。
- **qBittorrent 保存逻辑**：`internal/pkg/utils/path.go:GenerateDownloadPath(basePath, animeName)` 会在基础路径下再创建一层以番剧名 sanitized 后的子目录，即 `basePath/番剧名/`。

### 1.2 命名规范

项目内有两套命名体系：

#### A. 已整理（Renamed/Organized）文件

由 `internal/service/downloader/renamer.go:RenameService.GenerateFileName()` 生成，默认模板为：

```
${title}/Season ${season}/${title} S${seasonFormat}E${episodeFormat}
```

实际磁盘示例：

```
/Volumes/仓库/Bangumi/胆大党/Season 2/胆大党 S02E19.mp4
/Volumes/仓库/Bangumi/差点在迷宫深处被信任的伙伴杀掉.../Season 1/... S01E12.mp4
```

#### B. 未整理（Raw）文件

部分剧集仍保留原始发布组文件名，未被重命名。实际磁盘示例：

```
/Volumes/仓库/Bangumi/夏日重现/Season 1/
  [Nekomoe kissaten&VCB-Studio] Summer Time Rendering [01][Ma10p_1080p][x265_flac].mkv
```

#### C. 异常嵌套目录

由于 `GenerateDownloadPath()` 与模板 `${title}` 前缀叠加，部分文件出现了重复嵌套：

```
/Volumes/仓库/Bangumi/朋友的妹妹只缠著我/朋友的妹妹只缠著我/Season 1/朋友的妹妹只缠著我/Season 1/朋友的妹妹只缠著我 S01E01.mp4
```

### 1.3 数据库模型

- **`subscriptions.current_episode`**：已收集/已下载的集数（应反映磁盘实际最大集数）。
- **`subscriptions.latest_episode`**：从 RSS / Bangumi 获取的最新集数（源信息）。
- **`downloads` 表**：记录每个下载任务，关键字段包括 `subscription_id`、`episode`、`status`、`file_path`、`renamed_path`。

### 1.4 已发现的异常

通过 SQL 查询已确认存在多处不一致：

| 订阅名 | current_episode | latest_episode | 实际 completed 下载数 | 备注 |
|--------|-----------------|----------------|----------------------|------|
| 祭品公主与兽之王 | 24 | 24 | 0 | 无下载记录但 current=24 |
| 胆大党 | 28 | 12 | 0 | 磁盘上存在 S02E19-E24，但 DB 无记录 |
| 葬送的芙莉莲 | 28 | 28 | 0 | 无下载记录但 current=28 |
| 剑来 第二季 | 8 | 6 | 12 (max=18) | 磁盘集数超过 DB 记录 |
| 地狱模式 | 11 | 2 | 11 (max=12) | current / latest 均偏低 |

---

## 2. 扫描器设计

### 2.1 核心目标

扫描磁盘上的现有视频文件，提取集数，将数据库中的 `subscriptions.current_episode`、`subscriptions.latest_episode` 以及 `downloads` 表修正为与磁盘实际内容一致。

### 2.2 扫描范围与文件过滤

- **根目录**：`configs.download_path` 的值（运行时从数据库读取）。
- **递归深度**：不限（`filepath.WalkDir`）。
- **视频扩展名**：`.mkv`、`.mp4`、`.avi`、`.flv`、`.ts`、`.m2ts`、`.mov`、`.wmv`、`.webm`、`.m4v`（与现有 `FileMover` 一致）。
- **忽略文件**：
  - 隐藏文件（以 `.` 开头）。
  - 非视频文件。
  - 大小为 0 的空文件。

### 2.3 订阅匹配策略

采用**目录名优先 + 文件名回退**的双层匹配：

1. **第一层：目录名匹配**
   - 取文件相对根目录的第一级子目录名（如 `胆大党`、`夏日重现`）。
   - 使用 `internal/service/organizer/organizer_helper.go` 中的 `sanitizeDirectoryName()` 和 `isSimilarDirectoryName()` 与所有 `subscriptions.name` 进行比对。
   - 若匹配成功，直接归为该订阅。

2. **第二层：文件名解析回退**
   - 若目录名无法匹配任何订阅（可能是未按规范创建的文件夹），则使用现有的 `internal/service/organizer/parser.go:FileNameParser.Parse()` 解析文件名提取 `Title`。
   - 再通过 `internal/service/organizer/matcher.go:SubscriptionMatcher.Match()` 进行模糊匹配。
   - 匹配分数阈值设为 `0.7`，低于阈值则标记为 **未匹配孤儿文件**。

### 2.4 集数提取的精确正则/逻辑

根据文件是否已被整理，分两条路径：

#### 路径 A：已整理文件（文件名含 `SxxExx`）

使用正则：

```go
seasonEpisodePattern := regexp.MustCompile(`[Ss](\d{1,2})[Ee](\d{1,3})`)
```

提取逻辑：

```go
matches := seasonEpisodePattern.FindStringSubmatch(filename)
if len(matches) == 3 {
    season, _ := strconv.Atoi(matches[1])
    episode, _ := strconv.Atoi(matches[2])
    // season 用于校验订阅的 Season 字段
    // episode 为最终集数
}
```

#### 路径 B：未整理文件（原始发布组文件名）

直接复用现有逻辑 `internal/service/organizer/parser.go:FileNameParser.extractEpisode()`，其内部包含以下模式（按优先级顺序）：

```go
patterns := []string{
    `第?\s*(\d+)\s*[集话話]`,           // 第12集, 12话, [01]
    `[Ee][Pp]?\.?\s*(\d+)`,          // E12, EP12, Ep.12
    `Episode\s*(\d+)`,               // Episode 12
    `S\d{1,2}[Ee](\d+)`,             // S01E12
    `-\s*(\d{1,3})\s*[\[\-]`,        // - 12 [, - 01 -
    `\s+(\d{1,3})\s+\[`,             // 空格+数字+空格+[
}
```

额外处理：对于 `[01]`、`[12]` 这类方括号包裹的纯数字（常见于 VCB-Studio 等发布组），上述第 5、6 条模式已能覆盖。若仍失败，增加兜底模式：

```go
`\[(\d{1,3})\](?!/)`  // 匹配 [01]、[12]，但排除路径分隔
```

---

## 3. 数据库对账与修正策略

### 3.1 整体流程

```
1. 扫描磁盘 -> 2. 提取 (subscription, season, episode) -> 3. 聚合每个订阅的集数集合
4. 对比 DB -> 5. 生成修正报告 -> 6. 执行更新（如启用 apply 模式）
```

### 3.2 `subscriptions` 表修正规则

对于每个订阅：

- 计算该订阅在磁盘上所有匹配到的**最大集数** `maxEpisodeOnDisk`。
- **`current_episode`**：
  - 若 `maxEpisodeOnDisk > current_episode`，则更新为 `maxEpisodeOnDisk`。
  - 若 `maxEpisodeOnDisk < current_episode`（DB 记录比磁盘多），则**更新为 `maxEpisodeOnDisk`**，因为目标是“反映现实”。
  - 若磁盘上没有任何该订阅的文件，则**保持 `current_episode` 不变**（避免误删手动标记的进度）。
- **`latest_episode`**：
  - 该字段理论上来自 RSS/Bangumi 源，但若 `maxEpisodeOnDisk > latest_episode`，说明磁盘上已经有比源信息更新的剧集（或源信息长期未更新）。
  - 策略：**同步更新为 `maxEpisodeOnDisk`**，否则 `current_episode` 可能长期大于 `latest_episode`，导致前端显示异常。

### 3.3 `downloads` 表修正规则

对于每个订阅的每一集在磁盘上找到的文件：

#### A. 避免重复

- 先查询 `downloads` 中该 `subscription_id` + `episode` 的所有记录。
- 若已存在 `status = 'completed'` 的记录：
  - 检查其 `renamed_path` 是否指向磁盘上的实际文件。
  - 若路径不一致，更新 `renamed_path` 为扫描到的实际路径（取第一个匹配文件）。
- 若存在同集数但 `status != 'completed'`（如 `failed`、`downloading`、`stalled`）的记录：
  - 将其 `status` 修正为 `completed`。
  - 更新 `file_path` 和 `renamed_path` 为磁盘路径。
  - 设置 `downloaded_at = NOW()`。

#### B. 处理缺失记录（Orphans）

- 若磁盘上存在某集文件，但 `downloads` 表中完全没有任何该 `subscription_id` + `episode` 的记录：
  - **创建一条 synthetic 下载记录**：
    - `subscription_id` = 匹配到的订阅 ID
    - `title` = 文件名（或订阅名 + 集数）
    - `episode` = 提取的集数
    - `status` = `completed`
    - `file_path` / `renamed_path` = 磁盘绝对路径
    - `downloaded_at` = 扫描时间（或文件修改时间）
    - `torrent_hash` = 空字符串（因为无法回溯）
    - `fansub`、`language`、`resolution` = 尝试从文件名解析（复用 `FileNameParser`）

#### C. 处理 DB 中有但磁盘缺失的记录

- 对于 `status = 'completed'` 的下载记录，若其 `renamed_path`（或 `file_path`）指向的文件在磁盘上已不存在：
  - **不自动删除记录**，而是标记为 `missing` 状态或写入报告。
  - 原因：文件可能被手动移走、被磁盘清理删除，但记录仍有审计价值。
  - 若用户后续确认，可再执行清理。

---

## 4. 待修改/新增的文件清单

| # | 路径 | 操作 | 说明 |
|---|------|------|------|
| 1 | `internal/service/recovery/scanner.go` | **新建** | 扫描器核心服务：遍历磁盘、匹配订阅、提取集数、生成修正计划 |
| 2 | `internal/service/recovery/scanner_test.go` | **新建** | 单元测试：模拟文件名解析、目录匹配、对账逻辑 |
| 3 | `internal/api/handler/recovery.go` | **新建** | HTTP Handler：提供 `/api/v1/recovery/scan` 接口，支持 `dry_run` 参数 |
| 4 | `internal/api/router/router.go` | **修改** | 注册 Recovery Handler 路由（限定管理员/内部使用） |
| 5 | `internal/app/context.go` | **可选修改** | 若扫描器需要长期驻留在应用上下文中，则在此注册实例 |

**不修改的文件**（复用现有逻辑）：
- `internal/service/organizer/parser.go` — 复用 `FileNameParser`
- `internal/service/organizer/matcher.go` — 复用 `SubscriptionMatcher`
- `internal/service/organizer/organizer_helper.go` — 复用目录相似度判断
- `internal/pkg/utils/path.go` — 复用路径处理
- `internal/model/*.go` — 模型不变

---

## 5. 安全与回滚措施

### 5.1 数据库备份（强制）

扫描器执行任何写入前，必须：

1. 检查数据库文件路径（如 `./data/auto-rss.db`）。
2. 创建带时间戳的备份：`auto-rss.db.backup.YYYYMMDD_HHMMSS`。
3. 验证备份文件大小 > 0 后才继续。

### 5.2 默认只读（Dry Run）

- 扫描器默认以 **dry-run 模式**运行，仅生成 JSON 报告，**不写入数据库**。
- 只有在显式传入 `apply=true`（API 参数或 CLI flag）时，才执行真实更新。

### 5.3 事务包裹

- 所有数据库更新必须包裹在 `gorm.DB.Transaction` 中：
  - 先更新所有受影响的 `subscriptions`。
  - 再批量更新/插入 `downloads`。
  - 任一失败则整体回滚。

### 5.4 日志与审计

- 使用 `internal/pkg/logger` 记录每一条修正操作：
  - `INFO`：更新 subscription 的 current_episode / latest_episode（旧值 -> 新值）。
  - `INFO`：更新 download 状态为 completed（旧状态 -> 新状态）。
  - `INFO`：创建 synthetic download 记录。
  - `WARN`：发现 completed 记录对应的文件已丢失。

### 5.5 回滚指令

若执行后发现问题，用户可随时通过 SQLite 直接恢复：

```bash
# 停止 auto-rss 服务
# 替换回备份
cp data/auto-rss.db.backup.20260415_024500 data/auto-rss.db
# 重启服务
```

---

## 6. API 接口设计（预览）

```http
POST /api/v1/recovery/scan
Content-Type: application/json

{
  "dry_run": true,
  "subscription_id": null  // 可选，限定只扫描某个订阅
}
```

返回示例（dry-run）：

```json
{
  "code": 0,
  "data": {
    "scanned_files": 1247,
    "matched_files": 1189,
    "orphan_files": 12,
    "subscriptions": [
      {
        "id": 4,
        "name": "胆大党",
        "current_episode_old": 28,
        "current_episode_new": 24,
        "latest_episode_old": 12,
        "latest_episode_new": 24,
        "episodes_on_disk": [19,20,21,22,23,24],
        "downloads_to_update": 0,
        "downloads_to_create": 6,
        "downloads_missing": 0
      }
    ]
  }
}
```

---

## 7. 批准请求

**请在确认以下内容后回复批准：**

1. 同意上述扫描范围、订阅匹配策略和集数提取逻辑。
2. 同意对 `latest_episode` 的同步修正策略（当磁盘集数 > latest_episode 时更新）。
3. 同意为磁盘上存在但 DB 中缺失的剧集**创建 synthetic `downloads` 记录**（`torrent_hash` 为空）。
4. 同意对已 completed 但文件丢失的记录**保持不删除，仅报告**。
5. 知悉执行前会自动备份数据库，且默认 dry-run。

收到批准后，将立即开始编写代码。
