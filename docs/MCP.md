# Auto-RSS MCP 服务

Auto-RSS 可以通过 MCP Streamable HTTP 对外暴露一组给 AI agent 使用的管理工具。默认关闭，开启后端点为：

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
