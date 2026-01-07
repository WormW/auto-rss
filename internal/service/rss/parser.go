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

	"github.com/mmcdole/gofeed"
)

// Parser RSS 解析器接口
type Parser interface {
	// FetchAndParse 获取并解析 RSS Feed
	FetchAndParse(rssURL string) ([]RSSItem, error)
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
	TorrentURL  string
	TorrentHash string
	PubDate     string
	PubTime     time.Time // 解析后的发布时间
	Fansub      string
	Episode     int
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

// FetchAndParse 获取并解析 RSS Feed
func (p *parser) FetchAndParse(rssURL string) ([]RSSItem, error) {
	fp := gofeed.NewParser()
	fp.Client = p.httpClient

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	feed, err := fp.ParseURLWithContext(rssURL, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RSS feed: %w", err)
	}

	var items []RSSItem
	for _, item := range feed.Items {
		rssItem := RSSItem{
			Title:   item.Title,
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

		// 生成种子 Hash (使用 URL 的 MD5 作为临时 hash)
		if rssItem.TorrentURL != "" {
			hash := md5.Sum([]byte(rssItem.TorrentURL))
			rssItem.TorrentHash = fmt.Sprintf("%x", hash)
		}

		// 提取字幕组
		rssItem.Fansub = p.ExtractFansub(item.Title)

		// 提取集数
		rssItem.Episode = p.ExtractEpisode(item.Title)

		items = append(items, rssItem)
	}

	return items, nil
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
	patterns := []string{
		`第?\s*(\d+)\s*[集话話]`,           // 第12集, 12话
		`[Ee][Pp]?\.?\s*(\d+)`,          // E12, EP12, Ep.12
		`Episode\s*(\d+)`,               // Episode 12
		`\[\s*(\d+)\s*\]`,               // [12]
		`S\d+E(\d+)`,                    // S01E12
		`-\s*(\d+)\s*[-\[]`,             // - 12 -
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
