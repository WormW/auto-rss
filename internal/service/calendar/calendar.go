package calendar

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
)

// Calendar 追番日历服务
type Calendar struct {
	subscriptionRepo subscriptionRepository
	downloadRepo     downloadRepository
	notificationSvc  NotificationService
}

type subscriptionRepository interface {
	GetActiveSubscriptions() ([]model.Subscription, error)
}

type downloadRepository interface {
	GetBySubscriptionAndEpisode(subscriptionID uint, episode int) (*model.Download, error)
}

// NotificationService 通知服务接口
type NotificationService interface {
	Send(payload model.NotificationPayload)
}

// CalendarItem 日历条目
type CalendarItem struct {
	SubscriptionID uint   `json:"subscription_id"`
	Name           string `json:"name"`
	Episode        int    `json:"episode"`
	AirTime        string `json:"air_time"`
	AirDay         string `json:"air_day"`
	CurrentEpisode int    `json:"current_episode"`
	TotalEpisodes  int    `json:"total_episodes"`
	IsDownloaded   bool   `json:"is_downloaded"`
	IsCompleted    bool   `json:"is_completed"`
	Cover          string `json:"cover"`
}

// DaySchedule 每日排期
type DaySchedule struct {
	Day     string         `json:"day"`      // 星期几: monday, tuesday...
	DayCN   string         `json:"day_cn"`   // 中文: 周一, 周二...
	Items   []CalendarItem `json:"items"`    // 该日的番剧
	IsToday bool           `json:"is_today"` // 是否是今天
}

// WeekSchedule 每周排期
type WeekSchedule struct {
	Week string        `json:"week"` // current, next
	Days []DaySchedule `json:"days"` // 7天的排期
}

// dayNames 星期名称映射
var dayNames = map[string]string{
	"0": "sunday",
	"1": "monday",
	"2": "tuesday",
	"3": "wednesday",
	"4": "thursday",
	"5": "friday",
	"6": "saturday",
}

var dayNamesCN = map[string]string{
	"0": "周日",
	"1": "周一",
	"2": "周二",
	"3": "周三",
	"4": "周四",
	"5": "周五",
	"6": "周六",
}

// NewCalendar 创建日历服务
func NewCalendar(subscriptionRepo subscriptionRepository, downloadRepo downloadRepository) *Calendar {
	return &Calendar{
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
	}
}

// SetNotificationService 设置通知服务
func (c *Calendar) SetNotificationService(svc NotificationService) {
	c.notificationSvc = svc
}

// GetWeekSchedule 获取每周排期
func (c *Calendar) GetWeekSchedule(weekOffset int) (*WeekSchedule, error) {
	// 获取所有激活的订阅
	subscriptions, err := c.subscriptionRepo.GetActiveSubscriptions()
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}

	// 初始化7天的排期
	days := make([]DaySchedule, 7)
	now := time.Now()
	weekday := int(now.Weekday()) // 0=Sunday

	for i := 0; i < 7; i++ {
		dayIndex := (weekday + i) % 7
		dayKey := fmt.Sprintf("%d", dayIndex)
		days[i] = DaySchedule{
			Day:     dayNames[dayKey],
			DayCN:   dayNamesCN[dayKey],
			Items:   []CalendarItem{},
			IsToday: i == 0,
		}
	}

	// 将订阅分配到对应的星期
	for _, sub := range subscriptions {
		if sub.AirDay == "" {
			continue // 没有设置播出时间
		}
		if sub.IsCompleted() {
			continue // 日历仅展示未完结作品
		}

		// 检查是否已下载 - 查询下一集的下载状态
		isDownloaded := false
		nextOriginalEpisode := sub.CurrentEpisode + 1
		if c.downloadRepo != nil {
			if download, err := c.downloadRepo.GetBySubscriptionAndEpisode(sub.ID, nextOriginalEpisode); err == nil && download != nil {
				isDownloaded = download.Status == "completed"
			}
		}

		item := CalendarItem{
			SubscriptionID: sub.ID,
			Name:           sub.Name,
			Episode:        sub.RelativeEpisode(nextOriginalEpisode),
			AirTime:        sub.AirTime,
			AirDay:         sub.AirDay,
			CurrentEpisode: sub.RelativeCurrentEpisode(),
			TotalEpisodes:  sub.TotalEpisodes,
			IsDownloaded:   isDownloaded,
			IsCompleted:    false,
			Cover:          sub.BangumiCoverLocal,
		}

		// 找到对应的日期
		for i := 0; i < 7; i++ {
			dayIndex := (weekday + i) % 7
			if fmt.Sprintf("%d", dayIndex) == sub.AirDay {
				days[i].Items = append(days[i].Items, item)
				break
			}
		}
	}

	// 每天按播出时间排序
	for i := range days {
		sort.Slice(days[i].Items, func(a, b int) bool {
			return days[i].Items[a].AirTime < days[i].Items[b].AirTime
		})
	}

	weekLabel := "current"
	if weekOffset > 0 {
		weekLabel = "next"
	}

	return &WeekSchedule{
		Week: weekLabel,
		Days: days,
	}, nil
}

// GetTodaySchedule 获取今日排期
func (c *Calendar) GetTodaySchedule() ([]CalendarItem, error) {
	schedule, err := c.GetWeekSchedule(0)
	if err != nil {
		return nil, err
	}

	for _, day := range schedule.Days {
		if day.IsToday {
			return day.Items, nil
		}
	}

	return []CalendarItem{}, nil
}

// CheckUpcomingAiring 检查即将播出的番剧
// 返回距离播出时间小于提前提醒分钟数的番剧
func (c *Calendar) CheckUpcomingAiring() ([]CalendarItem, error) {
	today := time.Now()
	weekday := fmt.Sprintf("%d", int(today.Weekday()))

	// 获取今天播出的番剧
	subscriptions, err := c.subscriptionRepo.GetActiveSubscriptions()
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}

	var upcoming []CalendarItem

	for _, sub := range subscriptions {
		if !sub.NotifyEnabled || sub.AirDay != weekday {
			continue
		}
		if sub.IsCompleted() {
			continue
		}

		// 解析播出时间
		if sub.AirTime == "" {
			continue
		}

		airTime, err := time.Parse("15:04", sub.AirTime)
		if err != nil {
			continue
		}

		// 构建今天的播出时间
		now := time.Now()
		airDateTime := time.Date(now.Year(), now.Month(), now.Day(),
			airTime.Hour(), airTime.Minute(), 0, 0, now.Location())

		// 转换时区（如果是 JST，需要转换到本地时间）
		if sub.AirTimezone == "JST" {
			airDateTime = airDateTime.Add(-1 * time.Hour) // JST = UTC+9，简单处理
		}

		// 计算距离播出的时间差
		timeUntil := airDateTime.Sub(now)
		notifyBefore := time.Duration(sub.NotifyBeforeMin) * time.Minute

		// 如果距离播出时间在提醒范围内（即将播出或刚刚播出）
		if timeUntil > 0 && timeUntil <= notifyBefore {
			nextOriginalEpisode := sub.CurrentEpisode + 1
			upcoming = append(upcoming, CalendarItem{
				SubscriptionID: sub.ID,
				Name:           sub.Name,
				Episode:        sub.RelativeEpisode(nextOriginalEpisode),
				AirTime:        sub.AirTime,
				AirDay:         sub.AirDay,
				CurrentEpisode: sub.RelativeCurrentEpisode(),
				TotalEpisodes:  sub.TotalEpisodes,
				IsCompleted:    false,
			})
		}
	}

	return upcoming, nil
}

// SendAiringReminders 发送播出提醒
func (c *Calendar) SendAiringReminders() {
	upcoming, err := c.CheckUpcomingAiring()
	if err != nil {
		logger.Error("Failed to check upcoming airing", "error", err.Error())
		return
	}

	if len(upcoming) == 0 {
		return
	}

	logger.Info("Found upcoming airing episodes", "count", len(upcoming))

	for _, item := range upcoming {
		c.sendAiringNotification(item)
	}
}

// sendAiringNotification 发送播出通知
func (c *Calendar) sendAiringNotification(item CalendarItem) {
	if c.notificationSvc == nil {
		return
	}

	var episodeInfo string
	if item.Episode > 0 {
		episodeInfo = fmt.Sprintf("第 %d 集", item.Episode)
	} else {
		episodeInfo = "新一集"
	}

	c.notificationSvc.Send(model.NotificationPayload{
		Event:   model.EventAiringSoon,
		Title:   fmt.Sprintf("⏰ 即将更新: %s", item.Name),
		Message: fmt.Sprintf("%s %s 将在 %s 播出", item.Name, episodeInfo, item.AirTime),
		Data: map[string]any{
			"subscription_id": item.SubscriptionID,
			"name":            item.Name,
			"episode":         item.Episode,
			"air_time":        item.AirTime,
		},
		Timestamp: time.Now(),
	})
}

// SendNewEpisodeNotification 发送新集发布通知
func (c *Calendar) SendNewEpisodeNotification(sub *model.Subscription, episode int) {
	if c.notificationSvc == nil || !sub.NotifyEnabled {
		return
	}

	c.notificationSvc.Send(model.NotificationPayload{
		Event:   model.EventNewEpisode,
		Title:   fmt.Sprintf("🎬 %s 更新", sub.Name),
		Message: fmt.Sprintf("%s 第 %d 集已发布，开始下载...", sub.Name, episode),
		Data: map[string]any{
			"subscription_id": sub.ID,
			"name":            sub.Name,
			"episode":         episode,
		},
		Timestamp: time.Now(),
	})
}

// ParseAirDay 解析播出星期（支持多种格式）
func ParseAirDay(input string) string {
	// 支持格式：
	// 数字: "0", "1", "2"... (0=周日)
	// 中文: "周日", "周一", "周二"...
	// 英文: "sun", "mon", "tue"...
	// 完整: "sunday", "monday"...

	input = parseStandardDay(input)

	// 映射到数字
	dayMap := map[string]string{
		"sunday":    "0",
		"monday":    "1",
		"tuesday":   "2",
		"wednesday": "3",
		"thursday":  "4",
		"friday":    "5",
		"saturday":  "6",
	}

	if day, ok := dayMap[input]; ok {
		return day
	}

	// 尝试直接解析数字
	if _, err := strconv.Atoi(input); err == nil {
		return input
	}

	return ""
}

// parseStandardDay 将各种格式转换为标准星期名称
func parseStandardDay(input string) string {
	input = toLower(input)

	// 中文映射
	cnMap := map[string]string{
		"周日":  "sunday",
		"星期天": "sunday",
		"周一":  "monday",
		"周二":  "tuesday",
		"周三":  "wednesday",
		"周四":  "thursday",
		"周五":  "friday",
		"周六":  "saturday",
		"星期六": "saturday",
	}

	if day, ok := cnMap[input]; ok {
		return day
	}

	// 缩写映射
	abbrMap := map[string]string{
		"sun": "sunday",
		"mon": "monday",
		"tue": "tuesday",
		"wed": "wednesday",
		"thu": "thursday",
		"fri": "friday",
		"sat": "saturday",
	}

	if day, ok := abbrMap[input]; ok {
		return day
	}

	return input
}

// toLower 字符串转小写
func toLower(s string) string {
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
