package subscriptiondiscovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/rss"
	"github.com/WormW/auto-rss/internal/service/subscriptionmatch"
)

const userAgent = "WormW/Auto-RSS/0.3 (https://github.com/WormW/auto-rss)"
const responseLimit = 4 << 20

type cachedResponse struct {
	Body    []byte
	Expires time.Time
}
type HTTPProvider struct {
	Client      *http.Client
	BangumiBase string
	MikanBase   string
	NyaaBase    string
	DmhyBase    string
	mu          sync.Mutex
	cache       map[string]cachedResponse
	cacheBytes  int
	network     chan struct{}
	Proxy       func() string
}

func NewHTTPProvider(proxy func() string) *HTTPProvider {
	return &HTTPProvider{Client: &http.Client{Timeout: 12 * time.Second}, BangumiBase: "https://api.bgm.tv", MikanBase: "https://mikanime.tv", NyaaBase: "https://nyaa.si", DmhyBase: "https://share.dmhy.org", Proxy: proxy, cache: make(map[string]cachedResponse), network: make(chan struct{}, 1)}
}

func (p *HTTPProvider) get(ctx context.Context, address string, body []byte) ([]byte, error) {
	proxy := ""
	if p.Proxy != nil {
		proxy = p.Proxy()
	}
	key := proxy + "\n" + address + "\n" + string(body)
	p.mu.Lock()
	if cached, ok := p.cache[key]; ok && time.Now().Before(cached.Expires) {
		p.mu.Unlock()
		return cached.Body, nil
	}
	p.mu.Unlock()
	select {
	case p.network <- struct{}{}:
		defer func() { <-p.network }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Recheck after waiting: another preview may already have fetched this URL.
	p.mu.Lock()
	if cached, ok := p.cache[key]; ok && time.Now().Before(cached.Expires) {
		p.mu.Unlock()
		return cached.Body, nil
	}
	p.mu.Unlock()
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, application/rss+xml, text/html")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *p.Client
	if proxy != "" {
		parsed, e := url.Parse(proxy)
		if e != nil || parsed.Host == "" {
			return nil, fmt.Errorf("invalid system proxy")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(parsed)
		client.Transport = transport
		defer transport.CloseIdleConnections()
	}
	// Mikan currently redirects between its two known first-party hostnames.
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !allowedOrigin(req.URL, next.URL) {
			return fmt.Errorf("unexpected source redirect")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("source request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > responseLimit {
		return nil, fmt.Errorf("source response too large")
	}
	p.mu.Lock()
	if p.cache == nil || len(p.cache) >= 64 || p.cacheBytes+len(data) > 16<<20 {
		p.cache = make(map[string]cachedResponse)
		p.cacheBytes = 0
	}
	if previous, ok := p.cache[key]; ok {
		p.cacheBytes -= len(previous.Body)
	}
	p.cache[key] = cachedResponse{data, time.Now().Add(5 * time.Minute)}
	p.cacheBytes += len(data)
	p.mu.Unlock()
	return data, nil
}

func (p *HTTPProvider) Search(ctx context.Context, input PrepareInput) ([]Subject, error) {
	filter := map[string]any{"type": []int{2}}
	if input.Year > 0 {
		filter["air_date"] = []string{fmt.Sprintf(">=%d-01-01", input.Year), fmt.Sprintf("<%d-01-01", input.Year+1)}
	}
	body, _ := json.Marshal(map[string]any{"keyword": input.Query, "filter": filter})
	data, err := p.get(ctx, p.BangumiBase+"/v0/search/subjects?limit=5", body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []bangumi.Subject `json:"data"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid metadata response")
	}
	out := []Subject{}
	for _, s := range result.Data {
		if s.Type == bangumi.SubjectTypeAnime {
			out = append(out, toSubject(s))
		}
		if len(out) == 5 {
			break
		}
	}
	return out, nil
}

func (p *HTTPProvider) Subject(ctx context.Context, id int) (Subject, error) {
	data, err := p.get(ctx, fmt.Sprintf("%s/v0/subjects/%d", p.BangumiBase, id), nil)
	if err != nil {
		return Subject{}, err
	}
	var raw bangumi.Subject
	if err = json.Unmarshal(data, &raw); err != nil || raw.ID != id || raw.Type != bangumi.SubjectTypeAnime {
		return Subject{}, fmt.Errorf("selected subject is not an anime")
	}
	subject := toSubject(raw)
	if data, e := p.get(ctx, fmt.Sprintf("%s/v0/subjects/%d/subjects", p.BangumiBase, id), nil); e == nil {
		var relations []struct {
			ID       int    `json:"id"`
			Type     int    `json:"type"`
			Name     string `json:"name"`
			Relation string `json:"relation"`
		}
		if json.Unmarshal(data, &relations) == nil {
			for _, r := range relations {
				if r.Type == 2 {
					subject.Relations = append(subject.Relations, Relation{r.ID, r.Name, r.Relation})
					if len(subject.Relations) >= 8 {
						break
					}
				}
			}
		}
	}
	return subject, nil
}

func toSubject(raw bangumi.Subject) Subject {
	s := Subject{ID: raw.ID, Name: raw.Name, NameCN: raw.NameCN, Date: raw.Date, Platform: raw.Platform, Episodes: raw.Eps, URL: fmt.Sprintf("https://bgm.tv/subject/%d", raw.ID)}
	if s.Episodes < 0 || s.Episodes > 10000 {
		s.Episodes = 0
	}
	if raw.Images != nil {
		s.Cover = raw.Images.Large
	}
	s.Aliases = uniqueTitles([]string{raw.Name, raw.NameCN})
	for _, entry := range raw.Infobox {
		if entry.Key != "别名" {
			continue
		}
		switch value := entry.Value.(type) {
		case string:
			s.Aliases = append(s.Aliases, value)
		case []interface{}:
			for _, part := range value {
				if m, ok := part.(map[string]interface{}); ok {
					if alias, ok := m["v"].(string); ok {
						s.Aliases = append(s.Aliases, alias)
					}
				}
			}
		}
	}
	// Release titles commonly omit a subtitle after ～ or ': '. Derive only that
	// explicit prefix from the source's titles, and display it with the other aliases.
	for _, alias := range append([]string{}, s.Aliases...) {
		for _, separator := range []string{"～", "~", ": "} {
			if index := strings.Index(alias, separator); index >= 4 {
				s.Aliases = append(s.Aliases, strings.TrimSpace(alias[:index]))
				break
			}
		}
	}
	s.Aliases = uniqueTitles(s.Aliases)
	s.SeasonHint = subscriptionmatch.ExplicitSeason(raw.Name + " " + raw.NameCN)
	return s
}

func uniqueTitles(titles []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, title := range titles {
		title = strings.TrimSpace(title)
		key := strings.ToLower(title)
		if len(title) < 2 || len(title) > 300 || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, title)
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func (p *HTTPProvider) Feeds(ctx context.Context, subject Subject, input PrepareInput) ([]Feed, []string, error) {
	groups := [][]Feed{}
	warnings := []string{}
	for _, source := range input.Sources {
		var candidates []Feed
		switch source {
		case "mikan":
			found, err := p.mikanFeeds(ctx, subject, input)
			if err != nil {
				warnings = append(warnings, "Mikan: "+err.Error())
			} else {
				candidates = found
				if len(found) == 0 {
					warnings = append(warnings, "Mikan: 前 3 个搜索结果中没有找到带对应 Bangumi 链接的资源页。")
				}
			}
		case "nyaa":
			candidates = p.nyaaFeeds(subject, input)
		case "dmhy":
			candidates = p.dmhyFeeds(subject, input)
		}
		groups = append(groups, candidates)
	}
	// Give each requested source a turn before spending remaining preview slots.
	feeds := []Feed{}
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	for round := 0; len(feeds) < min(total, 6); round++ {
		for _, group := range groups {
			if round < len(group) && len(feeds) < 6 {
				feeds = append(feeds, group[round])
			}
		}
	}
	if total > 6 {
		warnings = append(warnings, "source_candidates_limited_to_6")
	}
	for i := range feeds {
		digest := sha256.Sum256([]byte(feeds[i].URL))
		feeds[i].ID = hex.EncodeToString(digest[:8])
	}
	return feeds, warnings, nil
}

func (p *HTTPProvider) nyaaFeeds(subject Subject, input PrepareInput) []Feed {
	queries := []string{}
	if input.NyaaQuery != "" {
		queries = append(queries, input.NyaaQuery)
	} else {
		// Prefer a source-provided Latin alias; the native title is a second bounded query.
		latin := regexp.MustCompile(`[A-Za-z]{3}`)
		best := ""
		for _, alias := range subject.Aliases {
			if latin.MatchString(alias) && (best == "" || len(alias) < len(best)) {
				best = alias
			}
		}
		if best != "" {
			queries = append(queries, best)
		}
		native := subject.Name
		for _, separator := range []string{"～", "~"} {
			if index := strings.Index(native, separator); index >= 4 {
				native = strings.TrimSpace(native[:index])
				break
			}
		}
		queries = uniqueTitles(append(queries, native))
		if len(queries) > 2 {
			queries = queries[:2]
		}
		for i, q := range queries {
			q = strings.NewReplacer("\"", " ", "|", " ").Replace(q)
			queries[i] = "\"" + q + "\""
		}
	}
	out := []Feed{}
	for _, query := range queries {
		// Resolution is verified locally too: 1920x1080 and 1080p both qualify.
		if input.NyaaQuery == "" && input.Resolution > 0 {
			query += " " + strconv.Itoa(input.Resolution)
		}
		params := url.Values{"page": {"rss"}, "q": {query}, "c": {"1_0"}, "f": {"0"}}
		if input.Language == "en" {
			params.Set("c", "1_2")
		}
		if input.Language == "chs" || input.Language == "cht" {
			params.Set("c", "1_3")
		}
		if input.TrustedOnly {
			params.Set("f", "2")
		}
		if input.NyaaUploader != "" {
			params.Set("u", input.NyaaUploader)
		}
		page := url.Values{}
		for k, v := range params {
			if k != "page" {
				page[k] = v
			}
		}
		out = append(out, Feed{Source: "nyaa", URL: p.NyaaBase + "/?" + params.Encode(), PageURL: p.NyaaBase + "/?" + page.Encode(), Query: query})
	}
	return out
}

func (p *HTTPProvider) mikanFeeds(ctx context.Context, subject Subject, input PrepareInput) ([]Feed, error) {
	name := input.Query
	data, err := p.get(ctx, p.MikanBase+"/Home/Search?searchstr="+url.QueryEscape(name), nil)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	pages := []string{}
	seen := map[string]bool{}
	pattern := regexp.MustCompile(`(?i)^/home/bangumi/([0-9]+)$`)
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if pattern.MatchString(href) && !seen[href] && len(pages) < 3 {
			seen[href] = true
			pages = append(pages, href)
		}
	})
	feeds := []Feed{}
	for _, path := range pages {
		data, err = p.get(ctx, p.MikanBase+path, nil)
		if err != nil {
			continue
		}
		page, e := goquery.NewDocumentFromReader(bytes.NewReader(data))
		if e != nil {
			continue
		}
		mapped := false
		page.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			u, e := url.Parse(href)
			if e == nil && (u.Hostname() == "bgm.tv" || u.Hostname() == "bangumi.tv" || u.Hostname() == "chii.in") && u.Path == fmt.Sprintf("/subject/%d", subject.ID) {
				mapped = true
			}
		})
		if !mapped {
			continue
		}
		page.Find(".leftbar-item").Each(func(_ int, group *goquery.Selection) {
			label := group.Find("a.subgroup-name")
			fansub := strings.TrimSpace(label.Text())
			if input.Fansub != "" && !strings.EqualFold(fansub, input.Fansub) {
				return
			}
			anchor, _ := label.Attr("data-anchor")
			if !strings.HasPrefix(anchor, "#") || len(anchor) > 100 {
				return
			}
			href, _ := page.Find(anchor).Find(".mikan-rss").First().Attr("href")
			feedURL, ok := p.mikanRSS(href)
			if !ok || len(feeds) >= 4 {
				return
			}
			feeds = append(feeds, Feed{Source: "mikan", URL: feedURL, PageURL: p.MikanBase + path, Fansub: fansub, BangumiID: subject.ID, MappingEvidence: "explicit_bangumi_link"})
		})
		if len(feeds) == 0 {
			match := pattern.FindStringSubmatch(path)
			feeds = append(feeds, Feed{Source: "mikan", URL: p.MikanBase + "/RSS/Bangumi?bangumiId=" + match[1], PageURL: p.MikanBase + path, BangumiID: subject.ID, MappingEvidence: "explicit_bangumi_link"})
		}
		break
	}
	return feeds, nil
}

func (p *HTTPProvider) mikanRSS(href string) (string, bool) {
	base, _ := url.Parse(p.MikanBase)
	parsed, err := url.Parse(href)
	if err != nil || !strings.EqualFold(parsed.Path, "/RSS/Bangumi") {
		return "", false
	}
	resolved := base.ResolveReference(parsed)
	if !allowedOrigin(base, resolved) || resolved.Query().Get("bangumiId") == "" {
		return "", false
	}
	return resolved.String(), true
}

func (p *HTTPProvider) Fetch(ctx context.Context, feed Feed) ([]rss.RSSItem, error) {
	var base string
	switch feed.Source {
	case "mikan":
		base = p.MikanBase
	case "nyaa":
		base = p.NyaaBase
	case "dmhy":
		base = p.DmhyBase
	default:
		return nil, fmt.Errorf("unsupported feed source")
	}
	want, _ := url.Parse(base)
	target, err := url.Parse(feed.URL)
	if err != nil || !allowedOrigin(want, target) {
		return nil, fmt.Errorf("unsupported feed origin")
	}
	data, err := p.get(ctx, feed.URL, nil)
	if err != nil {
		return nil, err
	}
	items, err := rss.NewParser().Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid RSS response")
	}
	if len(items) > 100 {
		items = items[:100]
	}
	if feed.Source == "dmhy" {
		// A classification failure should not discard a valid RSS preview. The
		// broad result remains usable; it simply has fewer team-evidence fields.
		if enriched, enrichErr := p.enrichDMHYGroups(ctx, feed, items); enrichErr == nil {
			items = enriched
		}
	}
	return items, nil
}

func allowedOrigin(base, target *url.URL) bool {
	if target.User != nil || target.Scheme != base.Scheme {
		return false
	}
	if target.Host == base.Host {
		return true
	}
	mikan := func(host string) bool { return host == "mikanime.tv" || host == "mikanani.me" }
	return base.Scheme == "https" && base.Port() == "" && target.Port() == "" && mikan(base.Hostname()) && mikan(target.Hostname())
}
