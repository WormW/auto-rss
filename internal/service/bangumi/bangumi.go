package bangumi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// BangumiService Bangumi API服务
type BangumiService struct {
	baseURL    string
	httpClient *http.Client
}

// NewBangumiService 创建Bangumi服务
func NewBangumiService() *BangumiService {
	return &BangumiService{
		baseURL: "https://api.bgm.tv",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SetProxy 设置代理
func (s *BangumiService) SetProxy(proxyURL string) error {
	if proxyURL == "" {
		s.httpClient.Transport = nil
		return nil
	}

	proxy, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}

	s.httpClient.Transport = &http.Transport{
		Proxy: http.ProxyURL(proxy),
	}
	return nil
}

// SubjectType 条目类型
type SubjectType int

const (
	SubjectTypeBook  SubjectType = 1
	SubjectTypeAnime SubjectType = 2
	SubjectTypeMusic SubjectType = 3
	SubjectTypeGame  SubjectType = 4
	SubjectTypeReal  SubjectType = 6
)

// Subject 番剧/条目信息
type Subject struct {
	ID      int         `json:"id"`
	Type    SubjectType `json:"type"`
	Name    string      `json:"name"`
	NameCN  string      `json:"name_cn"`  // 中文名
	Summary string      `json:"summary"`  // 简介
	Nsfw    bool        `json:"nsfw"`     // 是否NSFW
	Locked  bool        `json:"locked"`   // 是否锁定
	Date    string      `json:"date"`     // 发布日期
	Platform string     `json:"platform"` // 平台
	Images  *Images     `json:"images"`   // 封面图
	Infobox []InfoBox   `json:"infobox"`  // 详细信息
	Rating  *Rating     `json:"rating"`   // 评分
	Tags    []Tag       `json:"tags"`     // 标签
	Eps     int         `json:"eps"`      // 集数

	// 扩展字段
	Score       float64 `json:"score"`           // 评分(方便前端使用)
	TotalEps    int     `json:"total_episodes"`  // 总集数（修正为 total_episodes）
	AirDate     string  `json:"air_date"`        // 开播日期
	AirWeekday  int     `json:"air_weekday"`     // 开播星期
	Season      int     `json:"season"`          // 季度(从名称或infobox提取)
}

// Images 图片信息
type Images struct {
	Large  string `json:"large"`
	Common string `json:"common"`
	Medium string `json:"medium"`
	Small  string `json:"small"`
	Grid   string `json:"grid"`
}

// Rating 评分信息
type Rating struct {
	Rank  int     `json:"rank"`  // 排名
	Total int     `json:"total"` // 评分人数
	Count map[string]int `json:"count"` // 各分数人数
	Score float64 `json:"score"` // 评分
}

// Tag 标签
type Tag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// InfoBox 详细信息项
type InfoBox struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"` // 可能是string或array
}

// SearchResult 搜索结果
type SearchResult struct {
	Results int       `json:"results"`
	List    []Subject `json:"list"`
}

// Search 搜索番剧
func (s *BangumiService) Search(keyword string, subjectType SubjectType) (*SearchResult, error) {
	// 使用v0 API搜索 - POST请求
	searchURL := fmt.Sprintf("%s/v0/search/subjects", s.baseURL)

	// 构建请求体
	requestBody := map[string]interface{}{
		"keyword": keyword,
		"filter": map[string]interface{}{
			"type": []int{int(subjectType)},
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}

	req, err := http.NewRequest("POST", searchURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("User-Agent", "Auto-RSS/1.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Total int `json:"total"`
		Data  []Subject `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}

	// 转换为统一格式
	searchResult := &SearchResult{
		Results: result.Total,
		List:    result.Data,
	}

	// 处理每个结果
	for i := range searchResult.List {
		s.enrichSubject(&searchResult.List[i])
	}

	return searchResult, nil
}

// Episode 剧集信息
type Episode struct {
	ID          int    `json:"id"`
	Type        int    `json:"type"`
	Name        string `json:"name"`
	NameCN      string `json:"name_cn"`
	Sort        int    `json:"sort"`        // 集数
	EP          int    `json:"ep"`          // 章节编号
	AirDate     string `json:"airdate"`     // 播出日期
	Comment     int    `json:"comment"`     // 评论数
	Duration    string `json:"duration"`    // 时长
	Desc        string `json:"desc"`        // 描述
	Status      string `json:"status"`      // 状态
}

// GetSubjectEpisodes 获取番剧的剧集列表
func (s *BangumiService) GetSubjectEpisodes(subjectID int) ([]Episode, error) {
	// 使用正确的API端点：/v0/episodes?subject_id=xxx
	episodesURL := fmt.Sprintf("%s/v0/episodes", s.baseURL)

	// 构建查询参数
	params := url.Values{}
	params.Set("subject_id", strconv.Itoa(subjectID))
	params.Set("type", "0")     // 0 表示本篇
	params.Set("limit", "100")  // 获取最多100集
	params.Set("offset", "0")

	fullURL := fmt.Sprintf("%s?%s", episodesURL, params.Encode())

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("User-Agent", "Auto-RSS/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Total int       `json:"total"`
		Data  []Episode `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}

	return result.Data, nil
}

// GetLatestEpisodeSimple 获取番剧的参考集数（简化版）
// 对于已完结的番剧，返回总集数；对于正在播出的，返回0（依赖RSS源）
func (s *BangumiService) GetLatestEpisodeSimple(subject *Subject) int {
	// 如果有总集数信息，返回总集数作为参考
	if subject.TotalEps > 0 {
		return subject.TotalEps
	}
	return 0
}

// GetLatestEpisode 获取番剧最新已播出的集数
// 根据airdate字段判断：播出日期<=今天表示已播出
func (s *BangumiService) GetLatestEpisode(subjectID int) (int, error) {
	episodes, err := s.GetSubjectEpisodes(subjectID)
	if err != nil {
		// API不可用，返回错误
		return 0, err
	}

	maxEpisode := 0
	today := time.Now().Format("2006-01-02")

	for _, ep := range episodes {
		// 跳过特殊篇 (SP) 等
		if ep.Type != 0 {
			continue
		}

		// 如果没有播出日期，跳过
		if ep.AirDate == "" {
			continue
		}

		// 如果播出日期在今天或之前，认为已播出
		if ep.AirDate <= today {
			// 更新最大集数
			if ep.Sort > maxEpisode {
				maxEpisode = ep.Sort
			}
		}
	}

	return maxEpisode, nil
}

// GetSubject 获取番剧详情
func (s *BangumiService) GetSubject(id int) (*Subject, error) {
	detailURL := fmt.Sprintf("%s/v0/subjects/%d", s.baseURL, id)

	req, err := http.NewRequest("GET", detailURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("User-Agent", "Auto-RSS/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var subject Subject
	if err := json.NewDecoder(resp.Body).Decode(&subject); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}

	s.enrichSubject(&subject)

	return &subject, nil
}

// SearchByName 通过名称搜索番剧(自动匹配最佳结果)
func (s *BangumiService) SearchByName(name string) (*Subject, error) {
	result, err := s.Search(name, SubjectTypeAnime)
	if err != nil {
		return nil, err
	}

	if result.Results == 0 || len(result.List) == 0 {
		return nil, fmt.Errorf("no results found for: %s", name)
	}

	// 返回第一个结果(通常是最匹配的)
	bestMatch := &result.List[0]

	// 如果需要更详细的信息,获取完整详情
	if bestMatch.ID > 0 {
		detail, err := s.GetSubject(bestMatch.ID)
		if err == nil {
			return detail, nil
		}
		// 如果获取详情失败,返回搜索结果
	}

	return bestMatch, nil
}

// enrichSubject 丰富Subject数据
func (s *BangumiService) enrichSubject(subject *Subject) {
	// 设置Score字段
	if subject.Rating != nil {
		subject.Score = subject.Rating.Score
	}

	// 从Infobox提取信息
	for _, info := range subject.Infobox {
		switch info.Key {
		case "话数", "集数":
			if val, ok := info.Value.(string); ok {
				// 尝试解析集数
				var eps int
				fmt.Sscanf(val, "%d", &eps)
				if eps > 0 {
					subject.TotalEps = eps
				}
			}
		case "放送开始":
			if val, ok := info.Value.(string); ok {
				// 解析并转换中文日期格式
				subject.AirDate = parseChineseDate(val)
			}
		case "放送星期":
			if val, ok := info.Value.(string); ok {
				subject.AirWeekday = parseWeekday(val)
			}
		}
	}

	// 如果TotalEps还是0,使用Eps字段
	if subject.TotalEps == 0 && subject.Eps > 0 {
		subject.TotalEps = subject.Eps
	}

	// 提取季度信息
	subject.Season = extractSeasonFromName(subject.Name, subject.NameCN)
}

// parseWeekday 解析星期字符串
func parseWeekday(s string) int {
	s = strings.TrimSpace(s)
	weekdays := map[string]int{
		"星期日": 0, "星期天": 0, "周日": 0, "日": 0,
		"星期一": 1, "周一": 1, "一": 1,
		"星期二": 2, "周二": 2, "二": 2,
		"星期三": 3, "周三": 3, "三": 3,
		"星期四": 4, "周四": 4, "四": 4,
		"星期五": 5, "周五": 5, "五": 5,
		"星期六": 6, "周六": 6, "六": 6,
	}

	if day, ok := weekdays[s]; ok {
		return day
	}

	return -1 // 未知
}

// parseChineseDate 解析中文日期格式并转换为 ISO 格式
// 输入: "2025年4月6日" 或 "2023年9月29日"
// 输出: "2025-04-06" 或 "2023-09-29"
func parseChineseDate(s string) string {
	s = strings.TrimSpace(s)

	// 如果已经是ISO格式,直接返回
	if matched, _ := regexp.MatchString(`^\d{4}-\d{2}-\d{2}$`, s); matched {
		return s
	}

	// 解析中文日期格式: YYYY年MM月DD日
	re := regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日`)
	matches := re.FindStringSubmatch(s)

	if len(matches) == 4 {
		year := matches[1]
		month := matches[2]
		day := matches[3]

		// 补齐月份和日期为两位数
		if len(month) == 1 {
			month = "0" + month
		}
		if len(day) == 1 {
			day = "0" + day
		}

		return fmt.Sprintf("%s-%s-%s", year, month, day)
	}

	// 如果解析失败,返回原始字符串
	return s
}

// extractSeasonFromName 从名称中提取季度信息
func extractSeasonFromName(name, nameCN string) int {
	// 尝试从各种格式中提取季度
	patterns := []string{
		`第(\d+)季`,      // 第2季
		`第(\d+)期`,      // 第2期
		`Season\s*(\d+)`, // Season 2
		`S(\d+)`,         // S2
		` (\d+)期$`,      // 空格+数字+期
		`\s+(\d+)$`,      // 末尾的数字
	}

	texts := []string{name, nameCN}

	for _, text := range texts {
		if text == "" {
			continue
		}

		for _, pattern := range patterns {
			re := regexp.MustCompile(pattern)
			if matches := re.FindStringSubmatch(text); len(matches) > 1 {
				season, err := strconv.Atoi(matches[1])
				if err == nil && season > 0 {
					return season
				}
			}
		}
	}

	// 默认为第1季
	return 1
}
