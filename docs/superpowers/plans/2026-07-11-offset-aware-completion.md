# 偏移集数完结判定修复实施计划

> **供自动化执行者使用：** 必须使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans`，按任务逐项执行。所有步骤使用复选框跟踪状态。

**目标：** 让完结判定、完结统计和进度展示统一按照“原始集号减去集数偏移”计算，修复偏移 170、总集数 52 时被提前标记完结的问题。

**架构：** 保持数据库继续存储 RSS 原始集号，在订阅模型中集中提供相对集数与完结判定方法。智能拉取、日历和统计复用同一语义；前端通过独立的集数工具统一计算相对进度，避免页面内重复公式。

**技术栈：** Go、GORM、SQLite、Testify、Vue 3、TypeScript、Vite

---

## 文件结构

- 新建 `internal/model/subscription_test.go`：验证订阅模型的相对集数和完结边界。
- 修改 `internal/model/subscription.go`：提供相对当前集数、相对最新集数和完结状态方法。
- 修改 `internal/service/scheduler/smart_fetch_completed_test.go`：验证偏移判定和错误 `completed_at` 清理。
- 修改 `internal/service/scheduler/smart_fetch.go`：复用模型判定并清理旧错误状态。
- 修改 `internal/service/calendar/calendar_test.go`：验证偏移订阅不会提前从日历消失，展示集数为季度内相对集数。
- 修改 `internal/service/calendar/calendar.go`：使用模型完结判定，下载查询保留原始集号，输出使用相对集数。
- 修改 `internal/repository/subscription_stats_test.go`：验证完结统计考虑偏移。
- 修改 `internal/repository/subscription.go`：使用等价 SQL 统计相对集数达到总集数的订阅。
- 修改 `internal/service/bangumi/enrich_test.go`：验证 Bangumi 假完结纠正逻辑考虑偏移。
- 修改 `internal/service/bangumi/updater.go`：复用相对最新集数语义。
- 新建 `web/src/utils/episodes.ts`：集中计算前端相对集数、完结状态和进度百分比。
- 修改 `web/src/views/Subscriptions.vue`：完结筛选、进度条和进度文本使用相对集数。
- 修改 `docs/SMART_FETCH_FEATURE.md`：更新完结公式和偏移示例。

### 任务 1：订阅模型统一集数语义

**文件：**
- 新建：`internal/model/subscription_test.go`
- 修改：`internal/model/subscription.go`

- [ ] **步骤 1：编写失败的模型回归测试**

新增表格测试，覆盖以下输入：

```go
func TestSubscriptionEpisodeProgress(t *testing.T) {
	tests := []struct {
		name            string
		subscription    Subscription
		wantCurrent     int
		wantLatest      int
		wantCompleted   bool
	}{
		{
			name: "偏移订阅尚差一集",
			subscription: Subscription{
				EpisodeOffset:  170,
				TotalEpisodes:  52,
				CurrentEpisode: 221,
				LatestEpisode:  222,
			},
			wantCurrent:   51,
			wantLatest:    52,
			wantCompleted: false,
		},
		{
			name: "偏移订阅达到最后一集",
			subscription: Subscription{
				EpisodeOffset:  170,
				TotalEpisodes:  52,
				CurrentEpisode: 222,
				LatestEpisode:  222,
			},
			wantCurrent:   52,
			wantLatest:    52,
			wantCompleted: true,
		},
		{
			name: "无偏移保持原有行为",
			subscription: Subscription{
				TotalEpisodes:  12,
				CurrentEpisode: 12,
				LatestEpisode:  12,
			},
			wantCurrent:   12,
			wantLatest:    12,
			wantCompleted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.subscription.RelativeCurrentEpisode(); got != tt.wantCurrent {
				t.Fatalf("RelativeCurrentEpisode() = %d, want %d", got, tt.wantCurrent)
			}
			if got := tt.subscription.RelativeLatestEpisode(); got != tt.wantLatest {
				t.Fatalf("RelativeLatestEpisode() = %d, want %d", got, tt.wantLatest)
			}
			if got := tt.subscription.IsCompleted(); got != tt.wantCompleted {
				t.Fatalf("IsCompleted() = %v, want %v", got, tt.wantCompleted)
			}
		})
	}
}
```

- [ ] **步骤 2：运行测试确认失败**

运行：

```bash
go test ./internal/model -run TestSubscriptionEpisodeProgress -count=1
```

预期：编译失败，提示 `RelativeCurrentEpisode`、`RelativeLatestEpisode` 和 `IsCompleted` 尚未定义。

- [ ] **步骤 3：实现最小模型辅助方法**

在 `Subscription` 上新增：

```go
func (s Subscription) RelativeEpisode(originalEpisode int) int {
	relativeEpisode := originalEpisode - s.EpisodeOffset
	if relativeEpisode < 0 {
		return 0
	}
	return relativeEpisode
}

func (s Subscription) RelativeCurrentEpisode() int {
	return s.RelativeEpisode(s.CurrentEpisode)
}

func (s Subscription) RelativeLatestEpisode() int {
	return s.RelativeEpisode(s.LatestEpisode)
}

func (s Subscription) IsCompleted() bool {
	return s.TotalEpisodes > 0 && s.RelativeCurrentEpisode() >= s.TotalEpisodes
}
```

- [ ] **步骤 4：运行模型测试确认通过**

运行：

```bash
go test ./internal/model -run TestSubscriptionEpisodeProgress -count=1
```

预期：`ok`。

### 任务 2：修复智能拉取完结状态

**文件：**
- 修改：`internal/service/scheduler/smart_fetch_completed_test.go`
- 修改：`internal/service/scheduler/smart_fetch.go`

- [ ] **步骤 1：扩展完结表格测试并新增旧状态测试**

在 `TestIsCompleted` 中加入：

```go
{
	name: "偏移订阅尚未完结",
	sub: &model.Subscription{
		EpisodeOffset:  170,
		TotalEpisodes:  52,
		CurrentEpisode: 221,
	},
	expect: false,
},
{
	name: "偏移订阅已经完结",
	sub: &model.Subscription{
		EpisodeOffset:  170,
		TotalEpisodes:  52,
		CurrentEpisode: 222,
	},
	expect: true,
},
```

新增错误时间戳清理测试：

```go
func TestEvaluateSubscription_ClearsStaleCompletedAtForOffsetSubscription(t *testing.T) {
	filter := NewSmartFetchFilter(nil)
	completedAt := time.Now().Add(-40 * 24 * time.Hour)
	sub := &model.Subscription{
		Name:           "偏移订阅",
		EpisodeOffset:  170,
		TotalEpisodes:  52,
		CurrentEpisode: 171,
		CompletedAt:    &completedAt,
		AirDay:         "1",
	}

	status, needsUpdate := filter.EvaluateSubscription(sub)

	assert.False(t, status.IsCompleted)
	assert.True(t, needsUpdate)
	assert.Nil(t, sub.CompletedAt)
}
```

- [ ] **步骤 2：运行测试确认失败**

运行：

```bash
go test ./internal/service/scheduler -run 'TestIsCompleted|TestEvaluateSubscription_ClearsStaleCompletedAtForOffsetSubscription' -count=1
```

预期：偏移订阅被错误判定完结，且 `completed_at` 未被清理。

- [ ] **步骤 3：让智能拉取复用模型语义**

将 `isCompleted` 简化为：

```go
func (f *SmartFetchFilter) isCompleted(sub *model.Subscription) bool {
	return sub.IsCompleted()
}
```

在计算 `status.IsCompleted` 后、智能拉取开关提前返回前加入：

```go
if !status.IsCompleted && sub.CompletedAt != nil {
	sub.CompletedAt = nil
	needsUpdate = true
	logger.Info("Cleared stale subscription completion time",
		"subscription", sub.Name)
}
```

- [ ] **步骤 4：运行智能拉取测试确认通过**

运行：

```bash
go test ./internal/service/scheduler -run 'TestIsCompleted|TestEvaluateSubscription_ClearsStaleCompletedAtForOffsetSubscription|TestEvaluateSubscription_CompletedStopDays' -count=1
```

预期：全部通过。

### 任务 3：修复日历和完结统计

**文件：**
- 修改：`internal/service/calendar/calendar_test.go`
- 修改：`internal/service/calendar/calendar.go`
- 修改：`internal/repository/subscription_stats_test.go`
- 修改：`internal/repository/subscription.go`

- [ ] **步骤 1：编写日历偏移回归测试**

在日历测试中构造两个偏移订阅：原始第 221 集应继续显示，原始第 222 集应跳过。对未完结订阅设置：

```go
{
	ID:             4,
	Name:           "Offset Ongoing Anime",
	AirDay:         airDay,
	AirTime:        "13:00",
	EpisodeOffset:  170,
	CurrentEpisode: 221,
	TotalEpisodes:  52,
},
{
	ID:             5,
	Name:           "Offset Completed Anime",
	AirDay:         airDay,
	AirTime:        "14:00",
	EpisodeOffset:  170,
	CurrentEpisode: 222,
	TotalEpisodes:  52,
},
```

下载查询仍使用原始下一集：

```go
mockDownloadRepo.On("GetBySubscriptionAndEpisode", uint(4), 222).Return(nil, errors.New("not found"))
```

断言 `Offset Ongoing Anime` 的输出 `CurrentEpisode == 51`、`Episode == 52`，完结订阅不在结果中。

- [ ] **步骤 2：编写统计回归测试**

新增 `TestSubscriptionRepository_GetStatisticsUsesEpisodeOffset`，创建以下订阅：

```go
subscriptions := []model.Subscription{
	{Name: "普通完结", TotalEpisodes: 12, CurrentEpisode: 12},
	{Name: "偏移未完结", EpisodeOffset: 170, TotalEpisodes: 52, CurrentEpisode: 221},
	{Name: "偏移完结", EpisodeOffset: 170, TotalEpisodes: 52, CurrentEpisode: 222},
}
```

调用 `GetStatistics()` 并断言 `CompletedCount == 2`。

- [ ] **步骤 3：运行测试确认失败**

运行：

```bash
go test ./internal/service/calendar ./internal/repository -run 'TestGetWeekScheduleSkipsCompletedSubscriptions|TestSubscriptionRepository_GetStatisticsUsesEpisodeOffset' -count=1
```

预期：偏移未完结订阅被日历跳过，统计数量错误。

- [ ] **步骤 4：修复日历数据流**

使用 `sub.IsCompleted()` 跳过真正完结的订阅。下载查询继续使用：

```go
nextOriginalEpisode := sub.CurrentEpisode + 1
```

构造输出时使用：

```go
Episode:        sub.RelativeEpisode(nextOriginalEpisode),
CurrentEpisode: sub.RelativeCurrentEpisode(),
```

删除重复的 `isSubscriptionCompleted`，或保留为调用 `sub.IsCompleted()` 的薄封装。

- [ ] **步骤 5：修复统计 SQL**

将完结条件改为：

```go
Where("total_episodes > 0 AND MAX(current_episode - episode_offset, 0) >= total_episodes")
```

SQLite 的标量 `MAX(a, b)` 用于将相对集数下限限制为 0。

- [ ] **步骤 6：运行日历和统计测试确认通过**

运行：

```bash
go test ./internal/service/calendar ./internal/repository -run 'TestGetWeekScheduleSkipsCompletedSubscriptions|TestSubscriptionRepository_GetStatisticsUsesEpisodeOffset' -count=1
```

预期：全部通过。

### 任务 4：修复 Bangumi 假完结纠正逻辑

**文件：**
- 修改：`internal/service/bangumi/enrich_test.go`
- 修改：`internal/service/bangumi/updater.go`

- [ ] **步骤 1：编写偏移回归测试**

在 `TestShouldCorrectFalseCompletion` 增加：

```go
offsetSub := model.Subscription{
	EpisodeOffset:  170,
	TotalEpisodes:  52,
	LatestEpisode:  171,
}

assert.False(t, shouldCorrectFalseCompletion(offsetSub, 1))
```

这里已有最新原始集号 171 只代表季度内第 1 集，不应被视为“已到总集数后又回退到第 1 集”。

- [ ] **步骤 2：运行测试确认失败**

运行：

```bash
go test ./internal/service/bangumi -run TestShouldCorrectFalseCompletion -count=1
```

预期：返回 `true`，证明旧逻辑把原始集号 171 错当成超过总集数 52。

- [ ] **步骤 3：修复相对最新集数比较**

将判断改为：

```go
func shouldCorrectFalseCompletion(sub model.Subscription, latestEp int) bool {
	return sub.TotalEpisodes > 0 &&
		sub.RelativeLatestEpisode() >= sub.TotalEpisodes &&
		latestEp > 0 &&
		latestEp < sub.TotalEpisodes
}
```

- [ ] **步骤 4：运行测试确认通过**

运行：

```bash
go test ./internal/service/bangumi -run TestShouldCorrectFalseCompletion -count=1
```

预期：通过。

### 任务 5：统一前端完结与进度展示

**文件：**
- 新建：`web/src/utils/episodes.ts`
- 修改：`web/src/views/Subscriptions.vue`

- [ ] **步骤 1：新增共享集数工具**

创建：

```ts
import type { Subscription } from '@/api'

export const getRelativeEpisode = (episode = 0, offset = 0): number => {
  return Math.max(0, episode - offset)
}

export const getRelativeCurrentEpisode = (sub: Subscription): number => {
  return getRelativeEpisode(sub.current_episode || 0, sub.episode_offset || 0)
}

export const getRelativeLatestEpisode = (sub: Subscription): number => {
  return getRelativeEpisode(sub.latest_episode || 0, sub.episode_offset || 0)
}

export const isEpisodeProgressComplete = (sub: Subscription): boolean => {
  const total = sub.total_episodes || 0
  return total > 0 && getRelativeCurrentEpisode(sub) >= total
}

export const getEpisodeProgressPercent = (sub: Subscription): number => {
  const total = sub.total_episodes || 0
  if (total <= 0) return 0
  return Math.min(100, Math.round((getRelativeCurrentEpisode(sub) / total) * 100))
}
```

- [ ] **步骤 2：替换页面内重复公式**

导入上述工具。让 `isCompleted` 的当前集数判断调用 `isEpisodeProgressComplete`，日期和年份兜底判断使用 `getRelativeLatestEpisode(sub) >= total`。让 `getProgressPercent` 调用 `getEpisodeProgressPercent`，让 `isSeasonComplete` 调用 `isEpisodeProgressComplete`。

所有进度文本将 `sub.current_episode` 改为 `getRelativeCurrentEpisode(sub)`，所有“最新”季度集数展示改为 `getRelativeLatestEpisode(sub)`。下载记录和 API 中的原始集号不做修改。

- [ ] **步骤 3：运行前端类型检查与构建**

运行：

```bash
npm run build
```

工作目录：`web`

预期：`vue-tsc` 和 `vite build` 均成功退出。

### 任务 6：更新文档并完成全量验证

**文件：**
- 修改：`docs/SMART_FETCH_FEATURE.md`

- [ ] **步骤 1：更新智能拉取文档**

将完结公式更新为：

```text
RelativeCurrentEpisode = max(0, CurrentEpisode - EpisodeOffset)
RelativeCurrentEpisode >= TotalEpisodes 且 TotalEpisodes > 0
```

补充偏移 170、总集数 52 时，原始第 222 集才算完结的示例。

- [ ] **步骤 2：格式化并检查差异**

运行：

```bash
gofmt -w internal/model/subscription.go internal/model/subscription_test.go internal/service/scheduler/smart_fetch.go internal/service/scheduler/smart_fetch_completed_test.go internal/service/calendar/calendar.go internal/service/calendar/calendar_test.go internal/repository/subscription.go internal/repository/subscription_stats_test.go internal/service/bangumi/updater.go internal/service/bangumi/enrich_test.go
git diff --check
```

预期：无格式和空白错误。

- [ ] **步骤 3：运行完整 Go 测试**

运行：

```bash
go test ./... -count=1
```

预期：所有 Go 包测试通过。

- [ ] **步骤 4：重新运行前端生产构建**

运行：

```bash
npm run build
```

工作目录：`web`

预期：生产构建成功。

- [ ] **步骤 5：检查最终变更范围**

运行：

```bash
git status --short
git diff --stat
git diff --check
```

预期：仅包含本计划列出的代码、测试和文档文件，没有无关修改。
