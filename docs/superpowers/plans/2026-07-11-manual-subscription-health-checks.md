# 番剧健康诊断手动检查实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox ('- [ ]') syntax for tracking.

**Goal:** 将番剧健康诊断改为九个独立手动检查，并修复 RSS 代理、集数语义、qBittorrent 查询和重试删除 payload 问题。

**Architecture:** 保留诊断初始化 GET，但只返回未检查的项目定义；新增按 key 执行的 POST 接口。后端每项检查独立加载所需依赖，前端用纯函数合并单项结果并重新计算已检查汇总。

**Tech Stack:** Go 1.22、Gin、GORM、SQLite、Vue 3、TypeScript、Naive UI、Node test runner。

---

## 文件边界

- internal/service/rss/health.go、health_test.go：RSS 健康检查代理。
- internal/api/handler/subscription_diagnostics.go、subscription_diagnostics_test.go：初始化、单项检查、批量 qB 查询和安全重试。
- internal/api/router/router.go、router_test.go：单项检查路由。
- web/src/utils/subscription-diagnostics.ts、web/tests/subscription-diagnostics.test.ts：前端状态纯函数。
- web/src/api/index.ts、web/src/views/Subscriptions.vue、web/package.json：API、类型和手动检查面板。

### Task 1: RSS 健康检查代理

**Files:**
- Modify: internal/service/rss/health.go
- Create: internal/service/rss/health_test.go

- [ ] **Step 1: 写失败测试**

创建本地 HTTP proxy 返回有效 RSS，目标 URL 使用不可直连地址。设置代理后应健康，清空代理后 transport 不再使用代理：

~~~go
func TestRSSHealthChecker_SetProxyAndClear(t *testing.T) {
    proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/rss+xml")
        _, _ = io.WriteString(w, validHealthRSS("Proxy Anime"))
    }))
    defer proxy.Close()

    checker := NewHealthChecker(nil)
    require.NoError(t, checker.SetProxy(proxy.URL))
    got := checker.CheckSubscription(context.Background(), &model.Subscription{
        RssURL: "http://rss.invalid/feed",
    })
    require.Equal(t, HealthStatusHealthy, got.Status)
    require.NoError(t, checker.SetProxy(""))
}
~~~

- [ ] **Step 2: 验证 RED**

Run: go test ./internal/service/rss -run TestRSSHealthChecker_SetProxyAndClear -count=1

Expected: 编译失败，因为 SetProxy 尚不存在。

- [ ] **Step 3: 实现最小代理能力**

~~~go
func (c *RSSHealthChecker) SetProxy(proxyURL string) error {
    transport := &http.Transport{
        DialContext: (&net.Dialer{
            Timeout: 10 * time.Second,
            KeepAlive: 30 * time.Second,
        }).DialContext,
    }
    if strings.TrimSpace(proxyURL) != "" {
        parsed, err := url.Parse(proxyURL)
        if err != nil {
            return fmt.Errorf("invalid proxy URL: %w", err)
        }
        transport.Proxy = http.ProxyURL(parsed)
    }
    c.httpClient.Transport = transport
    return nil
}
~~~

- [ ] **Step 4: 验证 GREEN**

Run: go test ./internal/service/rss -count=1

Expected: PASS。

### Task 2: 后端单项诊断接口与安全修复

**Files:**
- Modify: internal/api/handler/subscription_diagnostics.go
- Modify: internal/api/handler/subscription_diagnostics_test.go
- Modify: internal/api/router/router.go
- Modify: internal/api/router/router_test.go

- [ ] **Step 1: 写初始化接口失败测试**

向 handler 传入 nil 下载仓储和 nil qB 客户端，GET 仍应成功，证明初始化不执行检查：

~~~go
handler := NewSubscriptionDiagnosticsHandler(subRepo, nil, configRepo, nil, t.TempDir())
resp := requestInitialDiagnostics(t, handler, sub.ID)
require.Equal(t, 0, resp.Summary.Checked)
require.Equal(t, 9, resp.Summary.Total)
require.Len(t, resp.Checks, 9)
for _, check := range resp.Checks {
    require.Equal(t, SubscriptionDiagnosticUnknown, check.Status)
}
~~~

- [ ] **Step 2: 写单项接口和未知 key 测试**

~~~go
r.POST("/subscriptions/:id/diagnostics/checks/:key", handler.Check)
ok := performRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/checks/subscription_enabled")
require.Equal(t, http.StatusOK, ok.Code)
bad := performRequest(r, http.MethodPost, "/subscriptions/1/diagnostics/checks/not-real")
require.Equal(t, http.StatusBadRequest, bad.Code)
~~~

- [ ] **Step 3: 写相对集数测试**

episode_offset=170、current_episode=221、latest_episode=222 时，episode_progress 返回待收集第 52 集：

~~~go
require.Equal(t, "episode_progress", result.Check.Key)
require.Equal(t, "待收集集数", result.Check.Label)
require.Equal(t, []int{52}, result.Files.PendingEpisodes)
~~~

- [ ] **Step 4: 写 qBittorrent 单次列表测试**

测试 client 记录方法调用次数：

~~~go
require.Equal(t, 1, qb.listCalls)
require.Equal(t, 0, qb.infoCalls)
require.Equal(t, 1, result.Downloads.MissingTorrentTasks)
~~~

- [ ] **Step 5: 写安全重试测试**

成功重试必须保留 payload；移除旧任务失败时不得修改数据库或重新添加：

~~~go
require.Equal(t, 1, qb.removeCalls)
require.Equal(t, 0, qb.deletePayloadCalls)

qb.removeErr = errors.New("remove failed")
response := retryFailed(t, handler)
require.Equal(t, 1, response.Failed)
require.Equal(t, 0, qb.addCalls)
updated, _ := downloadRepo.GetByID(download.ID)
require.Equal(t, model.DownloadStatusStalled, updated.Status)
require.Equal(t, "old-hash", updated.TorrentHash)
~~~

- [ ] **Step 6: 验证 RED**

Run: go test ./internal/api/handler -run 'TestSubscriptionDiagnosticsHandler_(GetInitialState|Check|Retry)' -count=1

Expected: 新字段、新接口或新行为缺失，测试失败。

- [ ] **Step 7: 实现初始化响应和单项执行器**

SubscriptionDiagnosticSummary 新增 Checked 和 Total。新增 SubscriptionDiagnosticCheckResponse，包含 Check 以及可选的 Downloads、Files、Disk、Actions。

Get 只加载订阅，使用固定九项定义生成 unknown 状态。Check 根据固定 key 只运行一个 builder，未知 key 返回 400。

执行 rss_reachability 前读取 system_proxy，并始终调用 SetProxy；缺少配置时传空字符串。

- [ ] **Step 8: 实现新集数语义**

~~~go
current := subscription.RelativeCurrentEpisode()
latest := subscription.RelativeLatestEpisode()
for episode := current + 1; episode <= latest; episode++ {
    pending = append(pending, episode)
}
~~~

检查键改为 episode_progress，标签改为“待收集集数”，详情说明这是订阅进度差。

- [ ] **Step 9: 实现批量 qB 检查与安全重试**

一次调用 GetTorrentsByCategory("") 建立 hash set。将 payload 删除改为：

~~~go
if err := h.qbClient.RemoveTorrentTask(download.TorrentHash); err != nil {
    return fmt.Errorf("移除旧下载任务失败: %w", err)
}
~~~

只有移除成功后才重置数据库并重新添加。

- [ ] **Step 10: 注册路由并验证 GREEN**

~~~go
subscriptions.POST("/:id/diagnostics/checks/:key", subscriptionDiagnosticsHandler.Check)
~~~

Run: go test ./internal/api/handler ./internal/api/router -count=1

Expected: PASS。

### Task 3: 前端手动检查状态与面板

**Files:**
- Create: web/src/utils/subscription-diagnostics.ts
- Create: web/tests/subscription-diagnostics.test.ts
- Modify: web/src/api/index.ts
- Modify: web/src/views/Subscriptions.vue
- Modify: web/package.json

- [ ] **Step 1: 写纯函数失败测试**

~~~ts
test('单项结果只替换对应检查并计算部分汇总', () => {
  const initial = createInitialDiagnostics(1, 'Anime')
  const next = mergeDiagnosticCheck(initial, {
    check: {
      key: 'rss_freshness',
      label: '最近检查',
      status: 'warning',
      summary: '2 天未检查',
      detail: ''
    }
  })
  assert.equal(next.summary.checked, 1)
  assert.equal(next.summary.total, 9)
  assert.equal(next.summary.overall, 'warning')
  assert.equal(
    next.checks.find(item => item.key === 'rss_reachability')?.status,
    'unknown'
  )
})
~~~

- [ ] **Step 2: 验证 RED**

Run: node --experimental-strip-types --test web/tests/subscription-diagnostics.test.ts

Expected: 模块不存在，测试失败。

- [ ] **Step 3: 实现纯函数和 API**

~~~ts
export function mergeDiagnosticCheck(
  current: SubscriptionDiagnostics,
  result: SubscriptionDiagnosticCheckResponse
): SubscriptionDiagnostics

export function summarizeDiagnosticChecks(
  checks: SubscriptionDiagnosticCheck[]
): SubscriptionDiagnostics['summary']
~~~

API 增加：

~~~ts
checkDiagnostic: (id: number, key: string) =>
  api.post('/subscriptions/' + id + '/diagnostics/checks/' + key)
~~~

- [ ] **Step 4: 验证纯函数 GREEN**

Run: node --experimental-strip-types --test web/tests/subscription-diagnostics.test.ts

Expected: PASS。

- [ ] **Step 5: 改造面板交互**

删除全局“刷新”按钮和打开面板后的全量检查。每项加入独立按钮：

~~~vue
<n-button
  size="tiny"
  :loading="diagnosticsCheckLoading[check.key]"
  :disabled="diagnosticsCheckLoading[check.key]"
  @click="runDiagnosticCheck(check.key)"
>
  {{ check.status === 'unknown' ? '检查' : '重新检查' }}
</n-button>
~~~

打开面板只调用初始化 GET。runDiagnosticCheck 调用单项 API 并用 mergeDiagnosticCheck 合并，不清除其他结果。修复动作完成后不再自动调用初始化或检查接口。

- [ ] **Step 6: 更新汇总和指标文案**

显示“已检查 N/9”；unknown 项显示“未检查”。“缺失集数”改为“待收集集数”，指标未检查时显示 --。不存在任何全部检查按钮。

- [ ] **Step 7: 加入测试脚本并验证**

~~~json
"test:diagnostics": "node --experimental-strip-types --test tests/subscription-diagnostics.test.ts"
~~~

Run: npm run test:diagnostics --prefix web

Expected: PASS。

Run: npm run build --prefix web

Expected: PASS。

### Task 4: 集成验证

**Files:**
- Modify only files above if verification exposes defects.

- [ ] **Step 1: 格式化**

Run: gofmt -w internal/service/rss/health.go internal/service/rss/health_test.go internal/api/handler/subscription_diagnostics.go internal/api/handler/subscription_diagnostics_test.go internal/api/router/router.go internal/api/router/router_test.go

- [ ] **Step 2: 运行聚焦测试**

Run: go test ./internal/service/rss ./internal/api/handler ./internal/api/router ./internal/service/downloader -count=1

Expected: PASS。

- [ ] **Step 3: 运行完整后端测试**

Run: go test ./... -count=1

Expected: PASS。

- [ ] **Step 4: 运行前端测试和构建**

Run: npm run test:episodes --prefix web

Run: npm run test:diagnostics --prefix web

Run: npm run build --prefix web

Expected: 全部 PASS。

- [ ] **Step 5: 检查最终差异**

Run: git diff --check

Run: git status --short

确认未修改无关文件，且用户已有改动保持不变。
