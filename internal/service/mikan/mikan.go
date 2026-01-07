package mikan

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// MikanService Mikan搜索服务
type MikanService struct {
	baseURL    string
	httpClient *http.Client
}

// NewMikanService 创建Mikan服务
func NewMikanService(baseURL string) *MikanService {
	if baseURL == "" {
		baseURL = "https://mikanime.tv"
	}
	return &MikanService{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // 30秒超时
		},
	}
}

// SetProxy 设置代理
func (s *MikanService) SetProxy(proxyURL string) error {
	if proxyURL == "" {
		s.httpClient.Transport = nil
		return nil
	}

	proxy, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}

	s.httpClient.Transport = &http.Transport{
		Proxy:                 http.ProxyURL(proxy),
		ResponseHeaderTimeout: 15 * time.Second,
	}
	return nil
}

// AnimeItem 番剧信息
type AnimeItem struct {
	Title  string  `json:"title"`
	URL    string  `json:"url"`
	Cover  string  `json:"cover"`
	Score  float64 `json:"score"`
	Exists bool    `json:"exists"`
	ID     string  `json:"id"`
}

// Season 季度信息
type Season struct {
	Year   int    `json:"year"`
	Season string `json:"season"`
	Select bool   `json:"select"`
}

// AnimeGroup 番剧分组
type AnimeGroup struct {
	Label string       `json:"label"`
	Items []*AnimeItem `json:"items"`
}

// SearchResult 搜索结果
type SearchResult struct {
	Groups  []*AnimeGroup `json:"groups"`
	Seasons []*Season     `json:"seasons"`
}

// Search 搜索番剧
func (s *MikanService) Search(searchText string) (*SearchResult, error) {
	searchURL := fmt.Sprintf("%s/Home/Search?searchstr=%s", s.baseURL, url.QueryEscape(searchText))
	return s.fetchAndParse(searchURL)
}

// GetBySeason 按季度获取番剧
func (s *MikanService) GetBySeason(year int, season string) (*SearchResult, error) {
	seasonURL := fmt.Sprintf("%s/Home/BangumiCoverFlowByDayOfWeek?year=%d&seasonStr=%s",
		s.baseURL, year, url.QueryEscape(season))
	return s.fetchAndParse(seasonURL)
}

// fetchAndParse 获取并解析页面
func (s *MikanService) fetchAndParse(pageURL string) (*SearchResult, error) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse HTML failed: %w", err)
	}

	result := &SearchResult{
		Groups:  []*AnimeGroup{},
		Seasons: []*Season{},
	}

	// 解析季度选择器
	s.parseSeasons(doc, result)

	// 解析番剧列表
	s.parseAnimeList(doc, result)

	return result, nil
}

// parseSeasons 解析季度列表
func (s *MikanService) parseSeasons(doc *goquery.Document, result *SearchResult) {
	dateSelect := doc.Find(".date-select").First()
	if dateSelect.Length() == 0 {
		return
	}

	currentText := dateSelect.Find(".date-text").Text()
	currentText = strings.TrimSpace(currentText)

	dateSelect.Find(".dropdown-menu li").Each(func(i int, li *goquery.Selection) {
		a := li.Find("a")
		if a.Length() == 0 {
			return
		}

		yearStr, _ := a.Attr("data-year")
		seasonStr, _ := a.Attr("data-season")

		year, err := strconv.Atoi(yearStr)
		if err != nil {
			return
		}

		seasonText := a.Text()
		isSelected := currentText == fmt.Sprintf("%s %s", yearStr, seasonText)

		result.Seasons = append(result.Seasons, &Season{
			Year:   year,
			Season: seasonStr,
			Select: isSelected,
		})
	})
}

// parseAnimeList 解析番剧列表
func (s *MikanService) parseAnimeList(doc *goquery.Document, result *SearchResult) {
	// 检查是否有分组
	skBangumis := doc.Find(".sk-bangumi")

	if skBangumis.Length() == 0 {
		// 搜索结果没有分组
		group := &AnimeGroup{
			Label: "搜索结果",
			Items: []*AnimeItem{},
		}
		s.parseAnimeItems(doc.Find(".an-ul").First(), group)
		result.Groups = append(result.Groups, group)
	} else {
		// 按星期分组
		skBangumis.Each(func(i int, bangumi *goquery.Selection) {
			label := bangumi.Children().First().Text()
			label = strings.TrimSpace(label)

			group := &AnimeGroup{
				Label: label,
				Items: []*AnimeItem{},
			}
			s.parseAnimeItems(bangumi, group)
			result.Groups = append(result.Groups, group)
		})
	}
}

// parseAnimeItems 解析番剧条目
func (s *MikanService) parseAnimeItems(container *goquery.Selection, group *AnimeGroup) {
	container.Find("li").Each(func(i int, li *goquery.Selection) {
		// 获取封面
		cover, _ := li.Find("span").Attr("data-src")
		if cover != "" && !strings.HasPrefix(cover, "http") {
			cover = s.baseURL + cover
		}

		// 获取标题和链接
		a := li.Find("a").First()
		if a.Length() == 0 {
			return
		}

		title := a.Text()
		title = strings.TrimSpace(title)

		href, _ := a.Attr("href")
		if href != "" && !strings.HasPrefix(href, "http") {
			href = s.baseURL + href
		}

		// 提取番剧ID
		idRegex := regexp.MustCompile(`\d+/?$`)
		id := idRegex.FindString(href)
		id = strings.TrimSuffix(id, "/")

		item := &AnimeItem{
			Title:  title,
			URL:    href,
			Cover:  cover,
			ID:     id,
			Score:  0.0,    // 评分需要从Bangumi API获取
			Exists: false,  // 需要检查数据库是否已存在
		}

		group.Items = append(group.Items, item)
	})
}

// FansubGroup 字幕组信息
type FansubGroup struct {
	Name      string   `json:"name"`
	RSS       string   `json:"rss"`
	UpdateDay string   `json:"update_day"`
	Tags      []string `json:"tags"`
	Episodes  []string `json:"episodes"`
}

// GetFansubGroups 获取番剧的字幕组列表
func (s *MikanService) GetFansubGroups(animeURL string) ([]*FansubGroup, error) {
	req, err := http.NewRequest("GET", animeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse HTML failed: %w", err)
	}

	groups := []*FansubGroup{}

	// 解析字幕组
	doc.Find(".leftbar-item").Each(func(i int, item *goquery.Selection) {
		group := &FansubGroup{
			Tags:     []string{},
			Episodes: []string{},
		}

		// 字幕组名称
		subgroupName := item.Find("a.subgroup-name")
		group.Name = strings.TrimSpace(subgroupName.Text())

		// RSS地址
		anchor, _ := subgroupName.Attr("data-anchor")
		if anchor != "" {
			rssLink := doc.Find(anchor).Find(".mikan-rss").First()
			rss, _ := rssLink.Attr("href")
			if rss != "" && !strings.HasPrefix(rss, "http") {
				rss = s.baseURL + rss
			}
			group.RSS = rss
		}

		// 更新日期
		updateDay := item.Find(".date").Text()
		group.UpdateDay = strings.TrimSpace(updateDay)

		// 解析集数和标签
		if anchor != "" {
			table := doc.Find(anchor).NextFiltered("table")
			table.Find("tbody tr").Each(func(j int, tr *goquery.Selection) {
				// 集数标题
				titleLink := tr.Find("a").First()
				title := titleLink.Text()
				title = strings.TrimSpace(title)

				// 提取集数
				episodeRegex := regexp.MustCompile(`第?(\d+)[集话話]|[Ee]p?\.?(\d+)|\[(\d+)]`)
				matches := episodeRegex.FindStringSubmatch(title)
				if len(matches) > 0 {
					for _, match := range matches[1:] {
						if match != "" {
							if !contains(group.Episodes, match) {
								group.Episodes = append(group.Episodes, match)
							}
							break
						}
					}
				}

				// 提取标签 (1080P, 简体, MP4等)
				tags := extractTags(title)
				for _, tag := range tags {
					if !contains(group.Tags, tag) {
						group.Tags = append(group.Tags, tag)
					}
				}
			})
		}

		groups = append(groups, group)
	})

	return groups, nil
}

// extractTags 从标题中提取标签
func extractTags(title string) []string {
	tags := []string{}

	// 分辨率
	resolutionPatterns := []string{"1080[Pp]", "720[Pp]", "4[Kk]", "2160[Pp]"}
	for _, pattern := range resolutionPatterns {
		if matched, _ := regexp.MatchString(pattern, title); matched {
			re := regexp.MustCompile(pattern)
			match := re.FindString(title)
			tags = append(tags, strings.ToUpper(match))
		}
	}

	// 语言
	if strings.Contains(title, "简体") || strings.Contains(title, "简中") || strings.Contains(title, "CHS") {
		tags = append(tags, "简体")
	}
	if strings.Contains(title, "繁体") || strings.Contains(title, "繁中") || strings.Contains(title, "CHT") {
		tags = append(tags, "繁体")
	}

	// 格式
	formats := []string{"MP4", "MKV", "AVI"}
	for _, format := range formats {
		if strings.Contains(strings.ToUpper(title), format) {
			tags = append(tags, format)
		}
	}

	return tags
}

// contains 检查字符串数组是否包含某个元素
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
