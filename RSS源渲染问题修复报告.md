# RSS 源管理渲染问题修复报告

## 问题描述

RSS 源管理页面无法正确渲染数据。

## 根本原因

**API 响应拦截器不一致**

在 `web/src/api/rss-source.ts` 中，使用了独立的 `axios` 实例，而不是使用 `api/index.ts` 中配置好的 `api` 实例。

### 问题代码

```typescript
// rss-source.ts - 错误的实现
import axios from 'axios'

const API_BASE = '/api/v1'

export const rssSourceApi = {
  list: (page: number, pageSize: number) => {
    return axios.get(`${API_BASE}/rss-sources`, { params: { page, page_size: pageSize } })
  },
  // ...
}
```

### 问题分析

`api/index.ts` 中配置了响应拦截器，会自动提取 `response.data`：

```typescript
api.interceptors.response.use(
  (response) => {
    return response.data  // 自动提取 data
  },
  // ...
)
```

但 `rss-source.ts` 使用的是原生 axios，返回的是完整的响应对象：

```json
{
  "data": {
    "data": {
      "list": [...],
      "total": 1
    }
  }
}
```

而前端组件期望的是：

```json
{
  "data": {
    "list": [...],
    "total": 1
  }
}
```

这导致前端尝试访问 `res.data?.list` 时得到 `undefined`。

## 修复方案

### 1. 修改 `api/index.ts` - 导出 api 实例

```typescript
// 将 api 从 const 改为 export const
export const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000
})
```

### 2. 修改 `rss-source.ts` - 使用统一的 api 实例

```typescript
// 修复后
import { api } from './index'

export const rssSourceApi = {
  list: (page: number, pageSize: number, enabled?: boolean) => {
    const params: any = { page, page_size: pageSize }
    if (enabled !== undefined) {
      params.enabled = enabled
    }
    return api.get('/rss-sources', { params })  // 使用统一的 api 实例
  },

  get: (id: number) => {
    return api.get(`/rss-sources/${id}`)
  },

  create: (data: Partial<RSSSource>) => {
    return api.post('/rss-sources', data)
  },

  update: (id: number, data: Partial<RSSSource>) => {
    return api.put(`/rss-sources/${id}`, data)
  },

  delete: (id: number) => {
    return api.delete(`/rss-sources/${id}`)
  },

  fetchAnimes: (id: number) => {
    return api.get(`/rss-sources/${id}/animes`)
  }
}
```

## 验证结果

### API 测试

```bash
curl -s http://localhost:7892/api/v1/rss-sources | jq .
```

**响应**：
```json
{
  "data": {
    "list": [
      {
        "id": 1,
        "name": "worm",
        "base_url": "https://mikanani.me/RSS/MyBangumi?token=xxx",
        "description": "",
        "enabled": true,
        "created_at": "2025-10-19T15:18:23.83752+08:00",
        "updated_at": "2025-10-19T15:18:23.83752+08:00"
      }
    ],
    "total": 1
  }
}
```

### 前端构建

```bash
✓ 2860 modules transformed.
dist/assets/index-S2WWVzaT.js   773.39 kB │ gzip: 226.14 kB
✓ built in 2.11s
```

## 修改文件清单

1. `web/src/api/index.ts` - 将 `api` 改为命名导出
2. `web/src/api/rss-source.ts` - 使用统一的 `api` 实例

## 技术要点

### 为什么需要响应拦截器？

1. **统一数据格式**：后端返回的是 `{ data: {...} }` 格式，拦截器自动提取 `data`
2. **简化组件代码**：组件不需要每次都 `.then(res => res.data.data)`
3. **统一错误处理**：在拦截器中集中处理错误

### 正确的 API 模式

**应该这样**：
```typescript
// 使用统一的 api 实例
import { api } from './index'
export const someApi = {
  list: () => api.get('/endpoint')
}
```

**不应该这样**：
```typescript
// 使用独立的 axios 实例
import axios from 'axios'
export const someApi = {
  list: () => axios.get('/api/v1/endpoint')
}
```

### 数据流对比

**修复前**：
```
后端响应: { data: { list: [...], total: 1 } }
↓ (原生 axios，无拦截器)
前端收到: { data: { data: { list: [...], total: 1 } } }
↓ (组件访问 res.data?.list)
结果: undefined ❌
```

**修复后**：
```
后端响应: { data: { list: [...], total: 1 } }
↓ (api 实例，有拦截器自动提取 response.data)
前端收到: { data: { list: [...], total: 1 } }
↓ (组件访问 res.data?.list)
结果: [...] ✅
```

## 经验教训

1. **保持 API 客户端一致性**：所有 API 调用应使用同一个配置好的实例
2. **响应拦截器的重要性**：统一处理响应格式和错误
3. **避免多个 axios 实例**：除非有明确的需求（如不同的 baseURL）
4. **TypeScript 类型检查局限**：这类运行时数据结构问题无法在编译时发现

## 总结

问题的根本原因是 API 客户端不一致导致响应数据结构不匹配。修复后，所有 API 调用都使用统一的 `api` 实例，确保响应拦截器生效，数据格式一致。

RSS 源管理页面现在应该能够正确渲染数据了。
