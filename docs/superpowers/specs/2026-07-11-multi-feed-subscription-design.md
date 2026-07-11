# 多 RSS Feed 订阅设计

## 读者与目标

本文面向负责实现和维护 Auto-RSS 的工程人员。读完后，应能够在同一个番剧订阅下配置多个字幕组 RSS feed，并确保不同 feed 使用各自的集数偏移、水位线和健康状态，同时仍以一份统一的剧集台账完成先到先得、去重和人工资源替换。

## 背景

当前订阅模型只有一个 `rss_url`、一个 `fansub`、一个 `episode_offset` 和一个 `last_rss_pub_time`。用户只能为一部番剧指定一个字幕组 RSS。当首选字幕组某天延迟或缺少资源，而另一个可接受的字幕组已经发布时，系统无法自动从后者获取该集。

简单创建两个独立订阅会分裂剧集进度、下载状态和资源候选，而且不同字幕组可能采用不同的原始集号。例如：

```text
A feed: 原始第 1 集，offset=0   -> 相对第 1 集
B feed: 原始第 101 集，offset=100 -> 相对第 1 集
```

因此，多 feed 必须共享同一个订阅和剧集台账，但分别保存源级配置和同步状态。

本设计依赖《剧集台账与人工资源替换设计》。该设计定义的 `subscription_episodes` 唯一约束和 `episode_resource_candidates` 是本功能的去重与候选处理边界。在剧集台账落地前，不应单独启用多 feed 自动下载。

## 目标

- 一个订阅可关联多个 RSS feed。
- 所有启用的 feed 地位相同，采用先到先得，不引入优先级或等待窗口。
- 每个 feed 独立配置集数偏移，并在进入剧集台账前转换为季度内相对集数。
- 每个 feed 独立维护发布时间水位线、基线同步状态、检查结果和健康信息。
- 同一相对集数只允许一个自动下载任务；后到的不同资源只建立人工候选。
- 新增 feed、修改 URL 或修改偏移时执行安全的基线同步，不自动回灌历史资源。
- 单个 feed 的超时、解析失败或非法集数不影响其他 feed。
- 兼容迁移现有单 RSS 订阅，不改变升级后的自动下载行为。
- 删除或停用 feed 不破坏剧集台账、下载历史和资源候选。

## 非目标

- 不为 feed 设置优先级、等待时间或首选/备用关系。
- 不自动推断集数偏移。
- 不为每个 feed 单独配置关键词、语言、画质或重命名规则。
- 不自动判断哪个字幕组或版本质量更高。
- 不自动替换已经下载或正在下载的同集资源。
- 不在本次设计中移除订阅表上的旧 RSS 兼容字段。

## 方案选择

### 多个独立订阅

每个字幕组建立一个订阅，再通过分组表达它们属于同一番剧。该方案不需要新的 feed 模型，但剧集台账仍以订阅为边界，无法天然阻止重复下载，也会分裂进度、缺集和候选资源，因此不采用。

### 订阅内保存 JSON feed 列表

把多个 URL、偏移和状态保存在订阅 JSON 字段中。初始改动较少，但 feed 需要独立更新水位线、错误和基线状态，JSON 会使事务更新、查询、约束和健康统计复杂化，因此不采用。

### 独立的 `subscription_feeds` 子表

为订阅增加一对多的 feed 子资源。订阅表示番剧及共享规则，feed 表示一个具体字幕组 RSS 入口及其映射和同步状态。该边界与实际生命周期一致，便于独立更新和测试，因此采用此方案。

项目现有 `rss_sources` 表表示 Mikan 等 RSS 服务商目录，不能复用为订阅 feed。新模型使用 `subscription_feeds`，避免服务商目录和实际订阅入口混淆。

## 数据模型

新增 `subscription_feeds` 表：

| 字段 | 含义 |
| --- | --- |
| `id` | 主键 |
| `subscription_id` | 所属订阅，建立索引和外键 |
| `name` | 用户可识别的 feed 名称 |
| `fansub` | 字幕组名称，可为空 |
| `rss_url` | 具体 RSS URL |
| `rss_url_normalized` | 用于判重的规范化 URL，不对用户展示 |
| `episode_offset` | 该 feed 的原始集数偏移，默认 0 |
| `enabled` | 是否参与周期检查 |
| `last_rss_pub_time` | 该 feed 的增量发布时间水位线 |
| `baseline_pending` | 是否需要执行首次/换源基线同步 |
| `last_check_time` | 最近一次检查时间 |
| `last_success_at` | 最近一次成功检查时间 |
| `last_error` | 最近一次检查错误，可为空 |
| `created_at` / `updated_at` | 审计时间 |

同一订阅下对 `(subscription_id, rss_url_normalized)` 建立唯一约束，防止重复添加完全相同的 feed。不同订阅可以使用相同 URL。URL 规范化规则必须由后端统一实现，至少处理首尾空白、scheme/host 大小写和无语义差异的默认端口；原始 `rss_url` 保留用户输入和实际请求所需的完整查询参数。

`episode_offset` 必须大于等于 0。条目转换规则为：

```text
relative_episode = original_episode - subscription_feed.episode_offset
```

转换结果小于等于 0 的条目无效，不进入剧集台账、候选或自动下载流程，并在预览或同步诊断中显示原因。

订阅级字段继续保存番剧身份和共享规则，包括季度、总集数、关键词、排除词、语言偏好、智能拉取和重命名配置。第一版不支持 feed 级过滤覆盖。

下载记录增加可空的 `subscription_feed_id`，表示该任务由哪个 feed 触发。资源候选保存 `subscription_feed_id`，同时保留发现时的 feed 名称、字幕组、URL、偏移等快照。删除 feed 后，历史记录仍可展示资源来源。

新增 `subscription_feed_seen_items` 运行时表，用于没有可靠发布时间的 RSS 快照去重：

| 字段 | 含义 |
| --- | --- |
| `id` | 主键 |
| `subscription_feed_id` | 所属 feed |
| `resource_key` | 优先使用小写 torrent hash，否则使用规范化资源 URL |
| `original_episode` | 发现时的原始集数 |
| `first_seen_at` | 首次观察时间 |

对 `(subscription_feed_id, resource_key)` 建立唯一约束。该表只表达“此 feed 已经展示过该资源”，不表达用户拥有该集，也不替代剧集台账或资源候选。它属于可重建运行时状态，不进入备份导出。

## Feed 生命周期

### 新增

用户必须先调用预览能力，确认 RSS 条目的原始集数和相对集数映射。保存后 feed 进入 `baseline_pending=true`，首次成功检查执行基线同步。

### 修改

规范化后的 RSS URL 发生变化，或 `episode_offset` 发生变化时：

- 设置 `baseline_pending=true`。
- 不沿用旧 URL 或旧映射的水位线。
- 清空该 feed 的已见资源快照，由新基线重新建立。
- 要求重新预览映射结果。

只修改 `name`、`fansub` 或 `enabled` 不触发基线同步。重新启用未改变 URL 和偏移的 feed 时继续使用原水位线。

### 停用

停用 feed 后停止周期拉取，但保留配置、水位线、健康信息和历史关联。停用不改变订阅和剧集台账状态。

### 删除

删除 feed 不删除剧集台账、下载记录或资源候选。下载记录上的可空外键置空，候选和历史展示继续使用保存的来源快照。删除 feed 不使已下载剧集恢复为缺失。

删除 feed 时同时删除它的 `subscription_feed_seen_items`；这些记录没有跨 feed 的历史展示价值。

## 基线同步

新增 feed、修改 URL 或修改偏移后的首次成功检查只建立新源基线，不自动下载 RSS 中的历史资源：

1. 使用 feed 自身的偏移把所有有效条目转换为相对集数。
2. 对已有剧集台账比较资源；资源不同则按剧集台账设计建立人工候选。
3. 对台账中不存在的历史集数建立 `missing` 条目，但不创建下载任务。
4. 为本次所有具有稳定资源身份的条目写入 `subscription_feed_seen_items`。
5. 把本次 RSS 中可见的最新发布时间保存为该 feed 的 `last_rss_pub_time`。
6. 设置 `baseline_pending=false`。

如果 RSS 条目没有可靠发布时间，仍以本次抓取快照完成基线。后续检查先查询 `subscription_feed_seen_items`：已见资源直接跳过，新资源才进入台账决策，并在处理成功后写为已见。这样第二轮检查不会把基线中已有的历史缺集误当成新发布资源。

基线同步失败时保持 `baseline_pending=true`，记录该 feed 的错误，其他 feed 继续正常运行。

## 增量处理与先到先得

基线完成后，有可靠发布时间的条目只处理发布时间超过自身水位线的内容；无可靠发布时间的条目只处理该 feed 尚未见过的资源 key。处理顺序为：

1. 解析 RSS 条目并提取原始集数。
2. 使用 feed 的 `episode_offset` 转换为相对集数。
3. 应用订阅级关键词、语言、智能拉取和总集数等过滤规则。
4. 在数据库事务中读取或创建 `(subscription_id, relative_episode)` 对应的剧集台账。
5. 当状态为无记录或 `missing` 时，原子地把该集占用为 `downloading` 并创建唯一下载任务。
6. 当状态为 `downloading`、`downloaded` 或 `marked_downloaded` 时，相同资源直接跳过，不同资源建立人工候选。
7. 当状态为 `ignored` 时直接跳过，不建立普通候选。
8. 条目完成跳过、候选或下载决策后，将其资源 key 幂等写入 feed 已见表。

多个 feed 没有优先级。最先成功提交台账状态变化和下载任务的资源胜出。不能依赖调度器遍历顺序表达优先级。

数据库必须依靠 `subscription_episodes(subscription_id, episode)` 唯一约束和带状态条件的原子更新处理并发。若两个 feed 几乎同时处理同一集，事务竞争失败的一方重新读取台账，并按后到资源规则跳过或创建候选，不能创建第二个下载任务。

下载和候选中的 `episode` 继续保留现有兼容语义时，应明确区分原始集数与台账相对集数。新代码不得假定不同 feed 的原始集数相同。至少应在内部处理对象中同时携带：

- `original_episode`
- `relative_episode`
- `subscription_feed_id`

## 水位线推进

每个 feed 独立推进 `last_rss_pub_time`。A feed 失败不能阻止 B feed 提交成功结果，也不能共用订阅级水位线。

水位线应在该 feed 的一次检查完成后推进到已成功扫描条目的最大发布时间。单个条目因为已有台账、成为候选或被订阅规则过滤，不应反复阻塞整个 feed 的水位线；解析失败且无法确定集数的条目应记录诊断信息，但不得影响其他 feed。

如果一次检查发生整体拉取或 XML 解析失败，不推进该 feed 水位线。

## API 设计

新增订阅下的 feed 子资源：

```text
GET    /api/v1/subscriptions/:id/feeds
POST   /api/v1/subscriptions/:id/feeds
PUT    /api/v1/subscriptions/:id/feeds/:feedId
DELETE /api/v1/subscriptions/:id/feeds/:feedId
POST   /api/v1/subscriptions/:id/feeds/preview
POST   /api/v1/subscriptions/:id/feeds/:feedId/preview
```

创建预览接收尚未保存的 `rss_url` 和 `episode_offset`。编辑预览默认使用当前 feed 配置，并允许请求中的待修改值覆盖。

预览结果至少包含：

```json
{
  "title": "RSS 原始标题",
  "original_episode": 101,
  "episode_offset": 100,
  "relative_episode": 1,
  "valid": true,
  "invalid_reason": ""
}
```

界面预览只负责让用户确认映射，不能作为服务端信任边界。创建 feed，以及修改 URL 或偏移时，服务端必须使用请求中的同一组 `rss_url` 和 `episode_offset` 再次执行拉取、解析和映射校验；校验成功后才持久化。这样即使客户端预览后修改了请求内容，也不能绕过映射验证。

## 界面设计

订阅创建和编辑界面把单个 RSS URL 区域替换为 RSS feed 列表。每行展示：

```text
启用状态 | 名称/字幕组 | RSS URL | 集数偏移 | 同步状态 | 最近检查结果 | 操作
```

新增和编辑 feed 使用独立表单。URL 或偏移发生变化时，保存前必须执行预览。预览表至少展示标题、原始集数、偏移、相对集数和无效原因。

订阅详情显示当前资源和候选资源的来源 feed。feed 错误只标记对应行，不把整个订阅显示为不可用；只有所有启用 feed 都不可用时，订阅汇总状态才显示无可用 feed。

第一版不提供 feed 排序所代表的业务优先级。界面排序仅用于展示，不能让用户误以为第一条 feed 会优先等待或下载。

## 兼容迁移

数据库迁移为每条已有且 `rss_url` 非空的订阅创建一条 feed：

| 新 feed 字段 | 旧订阅字段 |
| --- | --- |
| `subscription_id` | `subscriptions.id` |
| `name` | `fansub` 非空时使用 `fansub`，否则使用“默认 RSS” |
| `fansub` | `subscriptions.fansub` |
| `rss_url` | `subscriptions.rss_url` |
| `episode_offset` | `subscriptions.episode_offset` |
| `enabled` | 订阅当前可参与 RSS 检查时为 true |
| `last_rss_pub_time` | `subscriptions.last_rss_pub_time` |
| `baseline_pending` | false |

迁移后的第一条 feed 不进入基线同步，确保升级后继续按原水位线增量处理。迁移必须幂等，重复执行不能创建第二条相同 feed。

订阅表上的 `rss_url`、`fansub`、`episode_offset` 和 `last_rss_pub_time` 暂时保留用于兼容旧 API 和回滚，但新调度逻辑只以 `subscription_feeds` 为事实来源。实现期间必须定义统一的兼容读写策略，不能让新旧字段成为两个长期可独立编辑的数据源。建议旧 API 更新单 RSS 字段时只代理到唯一默认 feed；当订阅已有多条 feed 时，旧 API 拒绝有歧义的 RSS 字段更新，并提示使用 feed API。

## 错误处理与可观测性

- 拉取超时、HTTP 错误、XML 解析错误和映射错误按 feed 记录。
- 一个 feed 失败不终止同一订阅其他 feed 的检查。
- 日志至少包含 `subscription_id`、`subscription_feed_id`、feed 名称、原始集数和相对集数。
- 相对集数小于等于 0、超过订阅总集数或无法解析集数时记录明确跳过原因。
- 基线同步、增量同步、成功占用、并发竞争失败、相同资源跳过和候选创建使用可区分的结构化事件。
- 订阅健康摘要由启用 feed 汇总，但保留每个 feed 的独立详情。

## 测试策略

### 模型和迁移

- 旧单 RSS 订阅迁移为一条非基线状态的 feed。
- 重复运行迁移不会创建重复 feed。
- 同一订阅不能添加规范化后相同的 URL。
- 不同订阅允许使用相同 URL。
- 删除 feed 不删除剧集台账、下载记录或候选。
- 删除 feed 会删除其已见资源运行时记录。

### 集数映射

- A 的原始第 1 集、offset 0 与 B 的原始第 101 集、offset 100 映射到同一相对第 1 集。
- 相对集数小于等于 0 时被拒绝并给出原因。
- 不同 feed 的偏移互不影响。

### 基线与水位线

- 新 feed 首次同步只建立缺集和候选，不自动下载历史内容。
- 无发布时间的基线条目在第二次检查时仍不会自动下载；真正新增的资源 key 可以进入正常决策。
- 修改 URL 或偏移重新进入基线同步。
- 修改名称或字幕组不触发基线同步。
- 一个 feed 同步失败时不推进其水位线，其他 feed 可正常推进。

### 并发和候选

- 两个 feed 几乎同时发现同一相对集数时只创建一个下载任务。
- 竞争失败的不同资源创建一个去重后的人工候选。
- 后到的相同 hash 或相同规范化 URL 不创建候选。
- 已下载、手动标记和忽略状态遵循剧集台账既有规则。

### API 和界面

- 创建和修改 feed 的预览展示原始与相对集数。
- 未完成有效预览时不能保存 URL 或偏移变化。
- 单个 feed 错误不会把其他 feed 标记为失败。
- 多 feed 订阅通过旧单 RSS API 修改 URL 时返回明确的冲突错误。

## 实施顺序约束

本功能应在剧集台账核心能力可用后实施。推荐顺序为：

1. 完成剧集台账、资源候选和原子占用接口。
2. 增加 `subscription_feeds` 模型、迁移和仓储。
3. 把现有单 RSS 调度器改为遍历 feed，并将偏移和水位线下沉到 feed。
4. 增加基线同步、预览和 feed API。
5. 更新订阅界面和健康诊断。
6. 完成旧字段兼容收口和并发验证。

多 feed 调度切换必须一次完成事实来源转换，不能让部分路径读取订阅级 RSS、另一部分路径读取 feed 表，否则会产生重复下载和水位线不一致。
