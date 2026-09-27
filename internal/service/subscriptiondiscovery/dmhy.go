package subscriptiondiscovery

import (
	"bytes"
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/WormW/auto-rss/internal/service/rss"
)

// DMHY exposes the same keyword expression on its search page and RSS endpoint.
// Unlike Nyaa, it has no verified trusted-uploader or language filter here.
func (p *HTTPProvider) dmhyFeeds(subject Subject, input PrepareInput) []Feed {
	queries := []string{input.DmhyQuery}
	if input.DmhyQuery == "" {
		// Chinese release names are common on DMHY. Use source-provided names,
		// keeping the native title as a second independently reviewable query.
		queries = uniqueTitles([]string{dmhyTitle(subject.NameCN), dmhyTitle(subject.Name)})
		for i, query := range queries {
			queries[i] = strings.Join(strings.Fields(strings.NewReplacer("\"", " ", "|", " ", ":", " ").Replace(query)), " ")
		}
	}
	// DMHY does not match the bare token "1080" to "1080p" like Nyaa does.
	// Leave quality to local rules to retain both 1080p and 1920x1080 releases.
	feeds := []Feed{}
	seen := map[string]bool{}
	for _, query := range queries {
		if query == "" || seen[query] {
			continue
		}
		seen[query] = true
		if input.DmhyTeamID > 0 && !strings.Contains(strings.ToLower(query), "team_id:") {
			query += " team_id:" + strconv.Itoa(input.DmhyTeamID)
		}
		params := url.Values{"keyword": {query}}
		feeds = append(feeds, Feed{
			Source: "dmhy", Query: query,
			URL:     p.DmhyBase + "/topics/rss/rss.xml?" + params.Encode(),
			PageURL: p.DmhyBase + "/topics/list?" + params.Encode(),
			TeamID:  input.DmhyTeamID,
		})
	}
	return feeds
}

type dmhyGroupRef struct {
	id   string
	name string
}

var dmhyTeamPath = regexp.MustCompile(`(?i)^/topics/list/team_id/(\d+)$`)

// enrichDMHYGroups joins RSS release links to the team link shown on DMHY's
// HTML search result. The RSS author is useful evidence, but team_id is the
// stable selector that can be used for the next narrow query.
func (p *HTTPProvider) enrichDMHYGroups(ctx context.Context, feed Feed, items []rss.RSSItem) ([]rss.RSSItem, error) {
	if feed.Source != "dmhy" || feed.PageURL == "" || len(items) == 0 {
		return items, nil
	}
	base, err := url.Parse(p.DmhyBase)
	if err != nil {
		return items, err
	}
	pageURL, err := url.Parse(feed.PageURL)
	if err != nil || !allowedOrigin(base, pageURL) {
		return items, nil
	}
	groupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	body, err := p.get(groupCtx, pageURL.String(), nil)
	if err != nil {
		return items, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return items, err
	}
	byURL := map[string]dmhyGroupRef{}
	byTitle := map[string]dmhyGroupRef{}
	doc.Find("td.title").Each(func(_ int, cell *goquery.Selection) {
		releaseHref, ok := cell.Find("a[href*='/topics/view/']").First().Attr("href")
		if !ok {
			return
		}
		teamHref, ok := cell.Find("span.tag a[href*='/topics/list/team_id/']").First().Attr("href")
		if !ok {
			return
		}
		match := dmhyTeamPath.FindStringSubmatch(teamHref)
		if len(match) != 2 {
			return
		}
		teamName := strings.TrimSpace(cell.Find("span.tag a").First().Text())
		if teamName == "" {
			return
		}
		ref := dmhyGroupRef{id: match[1], name: teamName}
		byURL[normalizeDMHYURL(base, releaseHref)] = ref
		title := strings.TrimSpace(cell.Find("a[href*='/topics/view/']").First().Text())
		if title != "" {
			byTitle[title] = ref
		}
	})
	for index := range items {
		ref, ok := byURL[normalizeDMHYURL(base, items[index].ItemURL)]
		if !ok {
			ref, ok = byTitle[strings.TrimSpace(items[index].Title)]
		}
		if ok {
			items[index].SourceGroupID = ref.id
			items[index].SourceGroupName = ref.name
		}
	}
	return items, nil
}

func normalizeDMHYURL(base *url.URL, raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.String() == "" {
		return ""
	}
	resolved := base.ResolveReference(parsed)
	resolved.Fragment = ""
	return strings.ToLower(resolved.Scheme + "://" + resolved.Host + resolved.Path)
}

func dmhyTitle(title string) string {
	for _, separator := range []string{"～", "~", ": "} {
		if index := strings.Index(title, separator); index >= 4 {
			return strings.TrimSpace(title[:index])
		}
	}
	return strings.TrimSpace(title)
}
