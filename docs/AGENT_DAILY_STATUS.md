# Agent 每日追番查询

外部 Agent 使用只读工具 `get_daily_status` 即可汇总已订阅番剧的每日状态，无需直接访问数据库或媒体硬盘。接口不会拉取 RSS、添加下载或发送通知。

## MCP

端点为服务地址下的 `/mcp`，协议为 Streamable HTTP。使用部署中的 `MCP_TOKEN` 作为 Bearer token（与 Home 通知 token 不同）。客户端配置见 [MCP.md](MCP.md)。

查询上海时区今天，省略 `date`：

```json
{"name":"get_daily_status","arguments":{"timezone":"Asia/Shanghai"}}
```

查询指定日期：

```json
{"name":"get_daily_status","arguments":{"date":"2026-09-28","timezone":"Asia/Shanghai"}}
```

## REST

```http
GET /api/v1/daily/status?timezone=Asia%2FShanghai
GET /api/v1/daily/status?date=2026-09-28&timezone=Asia%2FShanghai
```

REST 沿用业务 API 认证；`AUTH_ENABLED=true` 时使用登录得到的 access token。成功返回 HTTP 200，结果在 `data` 中；非法日期或时区返回 400，数据库读取失败返回 500。

## 返回字段与使用口径

| 字段 | Agent 应如何解释 |
|---|---|
| `date`、`timezone`、`start_at`、`end_at` | 查询的自然日，含开始、不含结束；默认使用服务器本地时区，建议显式传 `Asia/Shanghai` |
| `due_today` | 当前启用、未收齐订阅的周排期推算；有播出时间时换算时区，只有星期时按原星期归类。`episode` 是下一集估计，不能宣称官方已确认更新 |
| `checked_today` | feed 的最后一次检查落在查询日；成功/失败数量按 feed 统计，不是当天检查次数 |
| `collected_today` | 当天新建下载任务或待复核候选，按订阅汇总；含任务数、候选数、季度内集号、最后创建时间。手动采集也计入；旧任务重试不计作新采集 |
| `downloaded_today` | 完成时间落在查询日、当前状态为 `completed` 的任务；不等于媒体库刷新成功 |
| `errors` | 上述 feed 或当日新建下载/候选的当前错误。下载/候选的 `at` 是记录创建时间，feed 的 `at` 是最后检查时间 |
| `notes` | 排期、保留数据和历史查询的限制，Agent 应保留这些不确定性 |

首次基线可能产生待复核候选。因此 `candidate_count>0` 不能解释为已经开始下载；`download_count>0` 也不能解释为下载完成。`due_today` 与 `collected_today` 不是互补集合：补集、延迟发布可能在排期之外出现。

数据库仅保留 feed 的最后检查和当前错误。后续检查会覆盖它们，历史记录被删除也会影响结果；该接口不是完整审计日志。查询昨天的 `checked_today=[]` 不能证明昨天未检查，`errors=[]` 不能证明昨天没有出错。

## 可交给 Agent 的指令

> 当用户询问今日追番状态时，调用 `get_daily_status`，显式使用 `Asia/Shanghai` 时区。按“预计更新、已采集、已完成、当前异常”汇总名称与集号。将仅有候选的记录标为“待复核候选”，不要说已下载。遵守返回的 `notes`，不要将排期当成官方发布事实，不要因历史空结果断言未检查。查询状态不需要调用 `refresh_rss`；用户明确要求立即刷新时才调用该写入工具。
