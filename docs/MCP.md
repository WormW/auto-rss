# Auto-RSS MCP 服务

Auto-RSS 是独立运行的后台服务，可通过 MCP Streamable HTTP 对外暴露一组给 AI agent 使用的管理工具。默认关闭，开启后端点为：

```text
POST /mcp
GET /mcp
DELETE /mcp
```

## 启用方式

```env
MCP_ENABLED=true
MCP_TOKEN=replace-with-a-long-random-token
MCP_ALLOWED_ORIGINS=http://localhost:7892,http://127.0.0.1:7892
```

`MCP_TOKEN` 是必填 Bearer token。所有 MCP 请求都需要：

```http
Authorization: Bearer replace-with-a-long-random-token
```

`MCP_ALLOWED_ORIGINS` 用逗号分隔，只影响带 `Origin` 请求头的浏览器请求。没有 `Origin` 的服务端 MCP 客户端请求会被允许，但仍然必须带 Bearer token。

## 客户端示例

以支持 Streamable HTTP 的 MCP 客户端为例：

```json
{
  "mcpServers": {
    "auto-rss": {
      "url": "http://localhost:7892/mcp",
      "headers": {
        "Authorization": "Bearer replace-with-a-long-random-token"
      }
    }
  }
}
```

## 可用工具

| 工具 | 读写 | 用途 |
|------|------|------|
| `get_system_overview` | 只读 | 查看订阅、下载和 RSS 源概览 |
| `list_subscriptions` | 只读 | 分页查询订阅，获取订阅 ID |
| `get_subscription` | 只读 | 查看单个订阅和最近下载 |
| `prepare_subscription` | 仅写草稿 | 按名称/季数查候选，选择 Bangumi ID 后生成蜜柑/Nyaa/动漫花园 RSS、规则和实际样本 |
| `confirm_subscription` | 写入 | 按人工复核的草稿版本和 feed 创建追新订阅，支持重复确认 |
| `create_subscription` | 写入 | 根据 RSS URL 创建订阅 |
| `toggle_subscription` | 写入 | 启用、禁用或切换订阅 |
| `list_downloads` | 只读 | 查询下载记录和失败任务 |
| `get_download` | 只读 | 查看单个下载任务 |
| `retry_download` | 写入 | 重置下载并尝试重新加入 qBittorrent |
| `refresh_rss` | 写入 | 立即触发一次异步 RSS 检查 |
| `search_mikan` | 只读 | 在 Mikan 搜索番剧 |
| `get_mikan_season` | 只读 | 按季度发现 Mikan 番剧 |
| `get_mikan_fansubs` | 只读 | 获取 Mikan 字幕组和 RSS URL |
| `search_bangumi` | 只读 | 搜索 Bangumi 元数据 |
| `get_bangumi_subject` | 只读 | 获取 Bangumi 条目详情 |
| `get_calendar` | 只读 | 查看今日或本周追番日历 |
| `list_logs` | 只读 | 查询近期日志 |

## 新增订阅与管理范围

自然语言订阅优先使用以下流程：

1. 对话中的 AI 将需求转换成 `prepare_subscription` 输入。例如 `{"query":"落第贤者的学院无双","season":1,"resolution":1080}`。未指定语言时默认为 `any`，不要自行添加简中、字幕组或 trusted 限制。
2. 查看返回的动画候选，结合名称、类型、开播时间选择 ID；再次准备时传入 `bangumi_id`。可以携带上一轮 `draft_id` 和 `revision` 更新同一草稿，也可以创建新的草稿。
3. 向用户展示条目链接、季数、feed、筛选条件、实际匹配与排除样本、未知项和“只追新”策略。动漫花园宽搜还会返回 `group_candidates`：按 `team_id` 汇总字幕组的资源数、集数覆盖、命中集数和标题样例。若要收窄到某个字幕组，把候选 `id` 作为 `dmhy_team_id` 再准备一版；不要让模型凭标题自行猜 ID。`confirmable=false` 的来源不能提交确认；季数映射无法确定时需要澄清。
4. 用户明确复核后，调用 `confirm_subscription`，传入 `draft_id`、`revision`、`feed_id`。这些值必须来自同一份已复核草稿，不能凭名称重新猜测。确认后建立历史基线，已有集数不会自动补下。

草稿有效期 30 分钟，修改后旧版本不能确认；同版本同 feed 重复确认返回同一个订阅，换 feed 或过期则需要重新准备。新的确认流程不替换现有手动创建入口。MCP 的工具说明要求客户端在确认前征得用户复核，服务端验证具体草稿版本；它不声称能验证客户端背后是否确实有人点击批准。

未指定画质时，`prepare_subscription` 默认使用 `quality_policy={"excluded_resolutions":[720],"preferred_resolution":1080,"fallback":"wait"}`，排除 720p 并等待 1080p。用户允许备用画质时将完整策略中的 `fallback` 改为 `allow`；优先级只覆盖同一 RSS 同次拉取，不自动替换已有下载。`resolution` 是硬要求，非零时覆盖默认策略；不要把“优先 1080p”翻译成 `resolution=1080`。显式策略整体覆盖默认值，必须包含三个字段。

`sources` 默认是 `["mikan","nyaa","dmhy"]`；只查动漫花园时传 `["dmhy"]`。可用 `dmhy_query` 调整其 `keyword` 表达式；它与 `nyaa_query` 分开。可传 `dmhy_team_id` 使用上一版宽搜返回的团队 ID，服务会追加 `team_id:数字` 并保留该选择到正式 RSS URL。动漫花园自动查询不拼接画质词，本地仍执行相同画质规则。三个来源共享 6 个预览名额，按来源轮流分配；站点失败保留其他来源结果。

第一版支持蜜柑、Nyaa、动漫花园，普通单集及 `any/chs/cht/en` 语言要求。复杂季数别名无法确定时保守跳过；不自动学习放宽规则，不支持在确认动作中补历史全集或修改已有订阅。筛选预览使用默认命名模板示意，实际整理沿用部署中的命名配置及文件扩展名。

`create_subscription` 与 REST 共用订阅创建流程：校验 feed 可访问且能够映射集数后，事务性写入订阅、feed 和剧集台账。无效 feed 返回错误，不留下半成品订阅。首次自动同步只建立历史基线；历史补集通过 REST 手动采集。

完整编辑、多 feed 和批量管理接口仍在 REST；当前 MCP 工具尚未覆盖这些操作。`list_logs` 查询数据库记录，返回的 `persistence_enabled` 表示是否正在持久化新日志。默认关闭新的数据库日志写入，运行日志由 stderr 提供；空结果不能证明刚发起的操作成功，按需设置 `LOG_DB_ENABLED=true`。

## 安全建议

- 不要把 `/mcp` 直接裸露到公网；放在反向代理、VPN 或内网后面。
- 使用长随机 `MCP_TOKEN`，并像 API 密钥一样管理。
- 如果通过浏览器型 MCP 客户端访问，设置精确的 `MCP_ALLOWED_ORIGINS`。
- 写入工具会改变 Auto-RSS 状态：创建订阅、启停订阅、重试下载、触发 RSS 刷新。

## 调试

先确认未授权请求会被拒绝：

```bash
curl -i http://localhost:7892/mcp
```

再用 MCP Inspector 或支持 Streamable HTTP 的客户端连接 `http://localhost:7892/mcp`，并设置 `Authorization` header。
