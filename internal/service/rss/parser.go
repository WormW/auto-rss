package rss

import (
	"context"
	"crypto/md5"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/mmcdole/gofeed"
)

// Parser RSS 解析器接口
type Parser interface {
	// FetchAndParse 获取并解析 RSS Feed（使用默认30秒超时）
	FetchAndParse(rssURL string) ([]RSSItem, error)
	// FetchAndParseWithTimeout 获取并解析 RSS Feed（带自定义超时）
	FetchAndParseWithTimeout(rssURL string, timeout time.Duration) ([]RSSItem, error)
	// Parse 从 io.Reader 解析 RSS Feed
	Parse(feed interface{}) ([]RSSItem, error)
	// ExtractFansub 从标题中提取字幕组名称
	ExtractFansub(title string) string
	// ExtractEpisode 从标题中提取集数
	ExtractEpisode(title string) int
	// SetProxy 设置代理
	SetProxy(proxyURL string) error
}

// RSSItem RSS 条目
type RSSItem struct {
	Title       string
	RssURL      string
	TorrentURL  string
	TorrentHash string
	PubDate     string
	PubTime     time.Time // 解析后的发布时间
	Fansub      string
	Episode     int
	Language    LanguageType // 语言类型
	LangKeyword string       // 匹配到的语言关键词（用于日志）
}

type parser struct {
	httpClient *http.Client
}

// NewParser 创建 RSS 解析器实例
func NewParser() Parser {
	return &parser{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
	}
}

// SetProxy 设置代理
func (p *parser) SetProxy(proxyURL string) error {
	if proxyURL == "" {
		// 清空代理
		p.httpClient.Transport = &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		}
		return nil
	}

	proxyURLParsed, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}

	p.httpClient.Transport = &http.Transport{
		Proxy: http.ProxyURL(proxyURLParsed),
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	return nil
}

// FetchAndParse 获取并解析 RSS Feed（使用默认30秒超时）
func (p *parser) FetchAndParse(rssURL string) ([]RSSItem, error) {
	return p.FetchAndParseWithTimeout(rssURL, 30*time.Second)
}

// FetchAndParseWithTimeout 获取并解析 RSS Feed（带自定义超时）
func (p *parser) FetchAndParseWithTimeout(rssURL string, timeout time.Duration) ([]RSSItem, error) {
	// Use default timeout if invalid (zero or negative)
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Enforce minimum timeout of 1 second to prevent DoS (T-03-04 mitigation)
	if timeout < 1*time.Second {
		timeout = 1 * time.Second
	}

	fp := gofeed.NewParser()
	fp.Client = p.httpClient

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	feed, err := fp.ParseURLWithContext(rssURL, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RSS feed: %w", err)
	}

	var items []RSSItem
	for _, item := range feed.Items {
		rssItem := p.feedItemToRSSItem(item)
		items = append(items, rssItem)
	}

	return items, nil
}

func (p *parser) feedItemToRSSItem(item *gofeed.Item) RSSItem {
	rssItem := RSSItem{
		Title:   item.Title,
		RssURL:  extractItemRSSURL(item),
		PubDate: item.Published,
	}

	// 解析发布时间
	if item.PublishedParsed != nil {
		rssItem.PubTime = *item.PublishedParsed
	}

	// 提取种子链接
	if item.Enclosures != nil && len(item.Enclosures) > 0 {
		rssItem.TorrentURL = item.Enclosures[0].URL
	} else if item.Link != "" {
		rssItem.TorrentURL = item.Link
	}

	// 优先使用 RSS 扩展字段中的 info-hash（如 nyaa:infoHash），其次尝试从 URL 提取。
	if extHash := utils.ExtractInfoHashFromExtensions(item.Extensions); extHash != "" {
		rssItem.TorrentHash = extHash
	} else if rssItem.TorrentURL != "" {
		rssItem.TorrentHash = utils.ExtractInfoHashFromTorrentURL(rssItem.TorrentURL)
		if rssItem.TorrentHash == "" {
			hash := md5.Sum([]byte(rssItem.TorrentURL))
			rssItem.TorrentHash = fmt.Sprintf("%x", hash)
		}
	}

	// 提取字幕组
	rssItem.Fansub = p.ExtractFansub(item.Title)

	// 提取集数
	rssItem.Episode = p.ExtractEpisode(item.Title)

	// 提取语言
	rssItem.Language, rssItem.LangKeyword = DetectLanguage(item.Title)

	return rssItem
}

// Parse 从 io.Reader 或 URL 解析 RSS Feed
func (p *parser) Parse(feed interface{}) ([]RSSItem, error) {
	fp := gofeed.NewParser()
	fp.Client = p.httpClient

	var gfeed *gofeed.Feed
	var err error

	switch f := feed.(type) {
	case string:
		// 如果是字符串，当作 URL 处理
		gfeed, err = fp.ParseURL(f)
	case interface{ Read([]byte) (int, error) }:
		// 如果是 io.Reader
		gfeed, err = fp.Parse(f)
	default:
		return nil, fmt.Errorf("unsupported feed type: %T", feed)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse RSS feed: %w", err)
	}

	var items []RSSItem
	for _, item := range gfeed.Items {
		items = append(items, p.feedItemToRSSItem(item))
	}

	return items, nil
}

func extractItemRSSURL(item *gofeed.Item) string {
	if item == nil {
		return ""
	}

	candidates := []string{
		item.Custom["rss_url"],
		item.Custom["rssUrl"],
		item.Custom["rss"],
		item.GUID,
		item.Link,
	}
	candidates = append(candidates, item.Links...)

	for _, value := range candidates {
		if rssURL := normalizeItemRSSURL(value); rssURL != "" {
			return rssURL
		}
	}

	return ""
}

func normalizeItemRSSURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	path := strings.ToLower(parsed.Path)
	if strings.Contains(path, "/rss/") || strings.HasSuffix(path, "/rss") || strings.Contains(strings.ToLower(parsed.RawQuery), "rss") {
		return parsed.String()
	}

	if bangumiID := extractMikanBangumiID(parsed.Path); bangumiID != "" {
		parsed.Path = "/RSS/Bangumi"
		parsed.RawQuery = "bangumiId=" + bangumiID
		parsed.Fragment = ""
		return parsed.String()
	}

	return ""
}

func extractMikanBangumiID(path string) string {
	re := regexp.MustCompile(`(?i)/(?:Home/)?Bangumi/(\d+)`)
	matches := re.FindStringSubmatch(path)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// ExtractFansub 从标题中提取字幕组名称
func (p *parser) ExtractFansub(title string) string {
	// 使用正则表达式 ^\[([^\]]+)\] 提取字幕组
	re := regexp.MustCompile(`^\[([^\]]+)\]`)
	matches := re.FindStringSubmatch(title)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// ExtractEpisode 从标题中提取集数
func (p *parser) ExtractEpisode(title string) int {
	// 常见集数格式:
	// - [xx] 第12集
	// - E12, EP12, Episode 12
	// - 12话, 12話
	// - S01E12
	// - Title - 176 (1080p)
	patterns := []string{
		`第?\s*(\d+)\s*[集话話]`,                    // 第12集, 12话
		`[Ee][Pp]?\.?\s*(\d+)`,                  // E12, EP12, Ep.12
		`Episode\s*(\d+)`,                       // Episode 12
		`\[\s*(\d+)\s*\]`,                       // [12]
		`S\d+E(\d+)`,                            // S01E12
		`-\s*(\d+)\s*(?:v\d+)?\s*(?:$|\[|\(|-)`, // - 12, - 12 v2, - 12 [
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(title)
		if len(matches) > 1 {
			episode, err := strconv.Atoi(matches[1])
			if err == nil && episode > 0 && episode < 1000 {
				return episode
			}
		}
	}

	return 0
}
