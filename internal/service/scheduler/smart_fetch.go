package scheduler

import (
	"fmt"
	"strconv"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
)

// SmartFetchStrategy 智能拉取策略配置
type SmartFetchStrategy struct {
	Enabled bool // 是否启用智能拉取

	// 时间窗口配置
	BeforeAirDay int // 更新日前 N 天开始拉取（默认1）
	AfterAirDay  int // 更新日后 N 天继续拉取（默认2）

	// 完结状态检查
	SkipCompleted     bool // 是否跳过已完结的
	CompletedStopDays int  // 完结后 N 天彻底停止检查（0 表示不停止）

	// 本地库存检查
	CheckLocalComplete bool // 是否检查本地是否完整

	// 拉取频率调整
	NormalInterval    time.Duration // 普通状态拉取间隔
	ActiveInterval    time.Duration // 活跃窗口期拉取间隔
	CompletedInterval time.Duration // 完结后拉取间隔
}

// DefaultSmartFetchStrategy 默认策略
func DefaultSmartFetchStrategy() *SmartFetchStrategy {
	return &SmartFetchStrategy{
		Enabled:            true,
		BeforeAirDay:       1,     // 更新日前1天
		AfterAirDay:        2,     // 更新日后2天
		SkipCompleted:      false, // 不跳过已完结（可能还有v2版本）
		CompletedStopDays:  30,    // 完结30天后彻底停止检查
		CheckLocalComplete: true,  // 检查本地完整性
		NormalInterval:     30 * time.Minute,
		ActiveInterval:     10 * time.Minute, // 活跃期更频繁
		CompletedInterval:  60 * time.Minute, // 完结后降低频率
	}
}

// SubscriptionFetchStatus 订阅拉取状态
type SubscriptionFetchStatus struct {
	Subscription      *model.Subscription
	ShouldFetch       bool          // 是否应该拉取
	FetchReason       string        // 拉取原因
	Explanation       string        // 面向用户的解释文本
	NextFetchInterval time.Duration // 建议下次拉取间隔
	MissingEpisodes   []int         // 缺少的集数（如果本地不完整）
	IsInActiveWindow  bool          // 是否在活跃窗口期
	IsCompleted       bool          // 是否已完结
	SmartFetchEnabled bool          // 本订阅本轮是否启用智能拉取
}

// SmartFetchFilter 智能拉取过滤器
type SmartFetchFilter struct {
	strategy     *SmartFetchStrategy
	downloadRepo repository.DownloadRepository
}

// NewSmartFetchFilter 创建智能拉取过滤器
func NewSmartFetchFilter(downloadRepo repository.DownloadRepository) *SmartFetchFilter {
	return &SmartFetchFilter{
		strategy:     DefaultSmartFetchStrategy(),
		downloadRepo: downloadRepo,
	}
}

// LoadConfigFromDB 从数据库加载配置
func (f *SmartFetchFilter) LoadConfigFromDB(configRepo repository.ConfigRepository) {
	if configRepo == nil {
		return
	}

	// 每次加载从默认值开始，避免被删除或缺失的配置沿用旧值
	f.strategy = DefaultSmartFetchStrategy()

	// 加载是否启用智能拉取
	if cfg, err := configRepo.Get("smart_fetch.enabled"); err == nil && cfg != nil {
		f.strategy.Enabled = parseSmartFetchBool(cfg.Value, f.strategy.Enabled)
	}

	// 加载更新日前天数
	if cfg, err := configRepo.Get("smart_fetch.before_air_day"); err == nil && cfg != nil {
		if val, err := strconv.Atoi(cfg.Value); err == nil && val >= 0 {
			f.strategy.BeforeAirDay = val
		}
	}

	// 加载更新日后天数
	if cfg, err := configRepo.Get("smart_fetch.after_air_day"); err == nil && cfg != nil {
		if val, err := strconv.Atoi(cfg.Value); err == nil && val >= 0 {
			f.strategy.AfterAirDay = val
		}
	}

	// 加载是否跳过已完结
	if cfg, err := configRepo.Get("smart_fetch.skip_completed"); err == nil && cfg != nil {
		f.strategy.SkipCompleted = parseSmartFetchBool(cfg.Value, f.strategy.SkipCompleted)
	}

	// 加载完结后停止检查的天数
	if cfg, err := configRepo.Get("smart_fetch.completed_stop_days"); err == nil && cfg != nil {
		if val, err := strconv.Atoi(cfg.Value); err == nil && val >= 0 {
			f.strategy.CompletedStopDays = val
		}
	}

	// 加载是否检查本地完整性
	if cfg, err := configRepo.Get("smart_fetch.check_local_complete"); err == nil && cfg != nil {
		f.strategy.CheckLocalComplete = parseSmartFetchBool(cfg.Value, f.strategy.CheckLocalComplete)
	}

	logger.Info("Smart fetch strategy loaded",
		"enabled", f.strategy.Enabled,
		"before_air_day", f.strategy.BeforeAirDay,
		"after_air_day", f.strategy.AfterAirDay,
		"skip_completed", f.strategy.SkipCompleted,
		"completed_stop_days", f.strategy.CompletedStopDays,
		"check_local_complete", f.strategy.CheckLocalComplete)
}

// SetStrategy 设置策略
func (f *SmartFetchFilter) SetStrategy(strategy *SmartFetchStrategy) {
	f.strategy = strategy
}

// FilterSubscriptions 过滤应该拉取的订阅
// 返回：拉取状态列表，需要更新的订阅索引列表
func (f *SmartFetchFilter) FilterSubscriptions(subscriptions []model.Subscription) ([]SubscriptionFetchStatus, []int) {
	var results []SubscriptionFetchStatus
	var needsUpdateIndexes []int

	for i := range subscriptions {
		status, needsUpdate := f.EvaluateSubscription(&subscriptions[i])
		results = append(results, status)
		if needsUpdate {
			needsUpdateIndexes = append(needsUpdateIndexes, i)
		}
	}

	return results, needsUpdateIndexes
}

// EvaluateSubscription 评估单个订阅是否应该拉取
// 返回：拉取状态，是否需要更新数据库
func (f *SmartFetchFilter) EvaluateSubscription(sub *model.Subscription) (SubscriptionFetchStatus, bool) {
	status := SubscriptionFetchStatus{
		Subscription:      sub,
		ShouldFetch:       false,
		SmartFetchEnabled: true,
	}
	needsUpdate := false

	if f.strategy == nil {
		f.strategy = DefaultSmartFetchStrategy()
	}

	status.IsCompleted = f.isCompleted(sub)
	effectiveEnabled := f.isSmartFetchEnabledForSubscription(sub)
	status.SmartFetchEnabled = effectiveEnabled
	if !effectiveEnabled {
		status.ShouldFetch = true
		status.FetchReason = "smart_fetch_disabled"
		status.Explanation = "智能拉取未启用，本轮按普通 RSS 检查执行。"
		status.NextFetchInterval = f.strategy.NormalInterval
		return status, needsUpdate
	}

	// 1. 检查是否已完结且跳过已完结
	isCompleted := status.IsCompleted

	// 1.1 如果刚完结（CompletedAt 未设置），设置完结时间
	if isCompleted && sub.CompletedAt == nil {
		now := time.Now()
		sub.CompletedAt = &now
		needsUpdate = true // 标记需要更新数据库
		logger.Info("Subscription marked as completed",
			"subscription", sub.Name,
			"completed_at", now.Format("2006-01-02 15:04:05"))
	}

	// 1.2 检查完结后是否超过停止检查天数
	if isCompleted && f.strategy.CompletedStopDays > 0 && sub.CompletedAt != nil {
		daysSinceCompleted := int(time.Since(*sub.CompletedAt).Hours() / 24)
		if daysSinceCompleted >= f.strategy.CompletedStopDays {
			status.ShouldFetch = false
			status.FetchReason = fmt.Sprintf("completed_%d_days_ago_stop_checking", daysSinceCompleted)
			status.Explanation = fmt.Sprintf("已完结 %d 天，超过停止检查阈值，本轮跳过；下次会低频复查。", daysSinceCompleted)
			status.NextFetchInterval = 7 * 24 * time.Hour // 一周后再次检查（以防万一）
			return status, needsUpdate
		}
	}

	if isCompleted && f.strategy.SkipCompleted {
		status.FetchReason = "completed_skipped"
		status.Explanation = "订阅已完结，且全局配置要求跳过已完结订阅。"
		status.NextFetchInterval = f.strategy.CompletedInterval
		return status, needsUpdate
	}

	// 2. 检查是否在活跃窗口期（更新日前后）
	inWindow, daysUntilAir := f.isInActiveWindow(sub)
	status.IsInActiveWindow = inWindow

	// 3. 检查本地完整性
	var missingEpisodes []int
	if f.strategy.CheckLocalComplete && sub.TotalEpisodes > 0 {
		missingEpisodes = f.getMissingEpisodes(sub)
		status.MissingEpisodes = missingEpisodes
	}

	// 决策逻辑
	switch {
	// 情况1：在活跃窗口期 + 未完结 -> 高频拉取
	case inWindow && !isCompleted:
		status.ShouldFetch = true
		status.FetchReason = fmt.Sprintf("active_window_%ddays_until_air", daysUntilAir)
		status.Explanation = activeWindowExplanation(daysUntilAir)
		status.NextFetchInterval = f.strategy.ActiveInterval

	// 情况2：不在窗口期 + 本地不完整 + 未完结 -> 普通拉取
	case !inWindow && len(missingEpisodes) > 0 && !isCompleted:
		status.ShouldFetch = true
		status.FetchReason = fmt.Sprintf("incomplete_missing_%d_episodes", len(missingEpisodes))
		status.Explanation = fmt.Sprintf("不在更新窗口内，但本地缺失 %d 集，将继续拉取补全。", len(missingEpisodes))
		status.NextFetchInterval = f.strategy.NormalInterval

	// 情况3：已完结 + 本地不完整 -> 低频拉取（补全）
	case isCompleted && len(missingEpisodes) > 0:
		status.ShouldFetch = true
		status.FetchReason = fmt.Sprintf("completed_but_incomplete_%d_missing", len(missingEpisodes))
		status.Explanation = fmt.Sprintf("订阅已完结但本地仍缺失 %d 集，将低频拉取补全。", len(missingEpisodes))
		status.NextFetchInterval = f.strategy.CompletedInterval

	// 情况4：已完结 + 本地完整 -> 跳过或极低频
	case isCompleted && len(missingEpisodes) == 0:
		status.ShouldFetch = false
		status.FetchReason = "completed_and_full"
		status.Explanation = "订阅已完结且本地集数完整，本轮跳过。"
		status.NextFetchInterval = 24 * time.Hour // 一天检查一次即可

	// 情况5：不在窗口期 + 本地完整 + 未完结 -> 等待窗口期
	case !inWindow && len(missingEpisodes) == 0 && !isCompleted:
		status.ShouldFetch = false
		status.FetchReason = fmt.Sprintf("waiting_for_window_%ddays", daysUntilAir)
		status.Explanation = fmt.Sprintf("当前不在更新窗口内，本地集数完整，预计 %d 天后进入拉取窗口。", daysUntilAir)
		status.NextFetchInterval = time.Duration(daysUntilAir*24) * time.Hour

	// 默认：保守拉取
	default:
		status.ShouldFetch = true
		status.FetchReason = "default_conservative"
		status.Explanation = "缺少足够的更新日或完整性信息，本轮保守执行拉取。"
		status.NextFetchInterval = f.strategy.NormalInterval
	}

	return status, needsUpdate
}

func (f *SmartFetchFilter) isSmartFetchEnabledForSubscription(sub *model.Subscription) bool {
	if sub.SmartFetchOverride == "always" {
		return true
	}
	if sub.SmartFetchOverride == "never" {
		return false
	}
	if sub.SmartFetchOverride == "follow" {
		return f.strategy.Enabled
	}
	if sub.SmartFetchEnabled != nil {
		return *sub.SmartFetchEnabled
	}
	return f.strategy.Enabled
}

func parseSmartFetchBool(value string, fallback bool) bool {
	switch value {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return fallback
	}
}

func activeWindowExplanation(daysUntilAir int) string {
	if daysUntilAir == 0 {
		return "今天是更新日，本轮会高频拉取。"
	}
	if daysUntilAir > 0 {
		return fmt.Sprintf("距离更新日还有 %d 天，已进入更新日前窗口，本轮会拉取。", daysUntilAir)
	}
	return fmt.Sprintf("更新日已过 %d 天，仍在更新后窗口，本轮会拉取。", -daysUntilAir)
}

// isCompleted 检查订阅是否已完结
func (f *SmartFetchFilter) isCompleted(sub *model.Subscription) bool {
	// 情况1：明确设置了总集数且当前集数 >= 总集数
	if sub.TotalEpisodes > 0 && sub.CurrentEpisode >= sub.TotalEpisodes {
		return true
	}

	// 情况2：没有设置总集数，无法判断是否完结，返回 false
	if sub.TotalEpisodes == 0 {
		return false
	}

	return false
}

// isInActiveWindow 检查是否在活跃窗口期
// 返回：是否在窗口期，距离更新日还有多少天（负数表示已过）
func (f *SmartFetchFilter) isInActiveWindow(sub *model.Subscription) (bool, int) {
	if sub.AirDay == "" {
		// 没有设置更新日，默认随时拉取
		return true, 0
	}

	today := time.Now()
	currentWeekday := int(today.Weekday()) // 0=周日

	// 解析订阅的更新日
	airWeekday := parseWeekday(sub.AirDay)
	if airWeekday < 0 {
		// 解析失败，默认随时拉取
		return true, 0
	}

	// 计算距离更新日的天数差
	daysUntilAir := airWeekday - currentWeekday
	if daysUntilAir < 0 {
		daysUntilAir += 7 // 下周
	}

	// 如果今天是更新日，daysUntilAir = 0
	// 如果更新日是明天，daysUntilAir = 1
	// 如果更新日是昨天，daysUntilAir = 6 (因为是下周的)

	// 活跃窗口：更新日前 N 天 到 更新日后 M 天
	// 需要考虑跨周的情况

	// 计算今天是否在活跃窗口内
	isInWindow := false

	// 情况1：更新日在本周内
	if daysUntilAir >= 0 {
		// 更新日是今天或未来几天
		if daysUntilAir <= f.strategy.BeforeAirDay {
			// 更新日前 N 天内
			isInWindow = true
		}
	} else {
		// 更新日已过，计算是否在后 N 天内
		daysSinceAir := -daysUntilAir
		if daysSinceAir <= f.strategy.AfterAirDay {
			isInWindow = true
			daysUntilAir = -daysSinceAir // 负数表示已过几天
		}
	}

	// 特殊处理：如果更新日是昨天，daysUntilAir 会计算为 6（下周）
	// 需要重新计算
	if daysUntilAir >= 7-f.strategy.AfterAirDay && daysUntilAir <= 6 {
		// 可能是更新日后的几天
		daysAfterAir := 7 - daysUntilAir
		if daysAfterAir <= f.strategy.AfterAirDay {
			isInWindow = true
			daysUntilAir = -daysAfterAir
		}
	}

	return isInWindow, daysUntilAir
}

// getMissingEpisodes 获取缺少的集数
func (f *SmartFetchFilter) getMissingEpisodes(sub *model.Subscription) []int {
	if sub.TotalEpisodes <= 0 {
		// 不知道总集数，无法判断
		return nil
	}
	if f.downloadRepo == nil {
		logger.Warn("Skipping local completeness check because download repository is nil",
			"subscription_id", sub.ID)
		return nil
	}

	// 获取已下载的集数
	downloads, err := f.downloadRepo.ListBySubscriptionID(sub.ID)
	if err != nil {
		logger.Error("Failed to get downloads for completeness check",
			"subscription_id", sub.ID,
			"error", err.Error())
		return nil
	}

	// 构建已下载集数集合
	downloadedEpisodes := make(map[int]bool)
	for _, d := range downloads {
		if d.Episode > 0 && (d.Status == "completed" || d.Status == "downloading") {
			downloadedEpisodes[d.Episode] = true
		}
	}

	// 计算偏移
	offset := sub.EpisodeOffset

	// 找出缺失的集数
	var missing []int
	for ep := 1; ep <= sub.TotalEpisodes; ep++ {
		actualEp := ep + offset
		if !downloadedEpisodes[actualEp] {
			missing = append(missing, ep)
		}
	}

	return missing
}

// parseWeekday 解析星期字符串为数字 (0-6)
func parseWeekday(day string) int {
	// 尝试直接解析数字
	if len(day) == 1 && day[0] >= '0' && day[0] <= '6' {
		return int(day[0] - '0')
	}

	// 中文映射
	cnMap := map[string]int{
		"周日": 0, "星期天": 0,
		"周一": 1,
		"周二": 2,
		"周三": 3,
		"周四": 4,
		"周五": 5,
		"周六": 6, "星期六": 6,
	}

	if val, ok := cnMap[day]; ok {
		return val
	}

	// 英文映射
	enMap := map[string]int{
		"sunday": 0, "sun": 0,
		"monday": 1, "mon": 1,
		"tuesday": 2, "tue": 2,
		"wednesday": 3, "wed": 3,
		"thursday": 4, "thu": 4,
		"friday": 5, "fri": 5,
		"saturday": 6, "sat": 6,
	}

	// 转换为小写比较
	lower := toLowerStr(day)
	if val, ok := enMap[lower]; ok {
		return val
	}

	return -1 // 解析失败
}

func toLowerStr(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		result[i] = c
	}
	return string(result)
}

// GetFetchSummary 获取拉取摘要（用于日志）
func (f *SmartFetchFilter) GetFetchSummary(statuses []SubscriptionFetchStatus) map[string]interface{} {
	var shouldFetch, shouldSkip int
	var inWindow, completed int
	var withMissing []string

	for _, s := range statuses {
		if s.ShouldFetch {
			shouldFetch++
		} else {
			shouldSkip++
		}

		if s.IsInActiveWindow {
			inWindow++
		}

		if f.isCompleted(s.Subscription) {
			completed++
		}

		if len(s.MissingEpisodes) > 0 {
			withMissing = append(withMissing,
				fmt.Sprintf("%s(%d集)", s.Subscription.Name, len(s.MissingEpisodes)))
		}
	}

	return map[string]interface{}{
		"total":        len(statuses),
		"should_fetch": shouldFetch,
		"should_skip":  shouldSkip,
		"in_window":    inWindow,
		"completed":    completed,
		"with_missing": withMissing,
	}
}
