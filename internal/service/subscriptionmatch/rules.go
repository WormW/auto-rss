// Package subscriptionmatch evaluates the same confirmed policy in previews and scheduling.
package subscriptionmatch

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/WormW/auto-rss/internal/service/rss"
)

type Rules struct {
	Version       int            `json:"version"`
	Titles        []string       `json:"titles"`
	Season        int            `json:"season"`
	ScopedFeed    bool           `json:"scoped_feed"`
	ScopedFeedURL string         `json:"scoped_feed_url,omitempty"`
	EpisodeOffset int            `json:"episode_offset"`
	TotalEpisodes int            `json:"total_episodes"`
	Language      string         `json:"language"`
	Resolution    int            `json:"resolution"`
	QualityPolicy *QualityPolicy `json:"quality_policy,omitempty"`
	Fansub        string         `json:"fansub,omitempty"`
	Required      []string       `json:"required,omitempty"`
	Excluded      []string       `json:"excluded,omitempty"`
	TrustedOnly   bool           `json:"trusted_only"`
}

type Decision struct {
	Action string `json:"action"` // match, reject, review
	Reason string `json:"reason"`
}

var seasonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bS(\d{1,2})(?:E\d|\b)`),
	regexp.MustCompile(`(?i)\bSeason\s*(\d{1,2})\b`),
	regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)\s+Season\b`),
	regexp.MustCompile(`第\s*([0-9一二三四五六七八九十]+)\s*[季期]`),
}
var batchPattern = regexp.MustCompile(`(?i)\bbatch\b|全集|合集|\bcomplete\b|\b\d{1,3}\s*[-~～]\s*\d{1,3}\b`)
var specialPattern = regexp.MustCompile(`(?i)\b(?:OVA|OAD|SP|Special|Movie|NCOP|NCED)\b|剧场版|劇場版|特别篇|特別篇`)
var seasonSuffixPattern = regexp.MustCompile(`(?i)^\s+(?:[2-9]|II|III|IV|V)\s*(?:-|\[)`)
var languagePatterns = map[string]*regexp.Regexp{
	"chs": regexp.MustCompile(`(?i)\b(?:chs|sc|gb)\b|简体|简中|简繁|简日|簡體`),
	"cht": regexp.MustCompile(`(?i)\b(?:cht|tc|big5)\b|繁体|繁體|繁中|简繁|繁日`),
	"en":  regexp.MustCompile(`(?i)\b(?:eng|english)\b`),
}
var resolutionPatterns = func() map[int]*regexp.Regexp {
	out := map[int]*regexp.Regexp{}
	for _, size := range []int{480, 720, 1080, 2160} {
		out[size] = regexp.MustCompile(fmt.Sprintf(`(?i)(?:\b%d(?:p|i)?\b|\b\d{3,4}x%d\b)`, size, size))
	}
	return out
}()

func ExplicitSeason(title string) int {
	for _, pattern := range seasonPatterns {
		if found := pattern.FindStringSubmatch(title); len(found) > 1 {
			if number, err := strconv.Atoi(found[1]); err == nil {
				return number
			}
			for i, value := range []string{"一", "二", "三", "四", "五", "六", "七", "八", "九", "十"} {
				if found[1] == value {
					return i + 1
				}
			}
		}
	}
	return 0
}

func Decode(raw string) (Rules, error) {
	var rules Rules
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return rules, fmt.Errorf("invalid discovery rules")
	}
	return rules, rules.Validate()
}

func (r Rules) Validate() error {
	if (r.Version != 1 && r.Version != 2) || r.Season < 1 || r.Season > 99 || r.EpisodeOffset < 0 || r.TotalEpisodes < 0 || r.TotalEpisodes > 10000 || len(r.Titles) == 0 || len(r.Titles) > 20 {
		return fmt.Errorf("invalid discovery identity rules")
	}
	for _, title := range r.Titles {
		if len(strings.TrimSpace(title)) < 2 || len(title) > 300 {
			return fmt.Errorf("invalid title alias")
		}
	}
	switch r.Language {
	case "any", "chs", "cht", "en":
	default:
		return fmt.Errorf("language must be any, chs, cht or en")
	}
	switch r.Resolution {
	case 0, 480, 720, 1080, 2160:
	default:
		return fmt.Errorf("unsupported resolution")
	}
	if r.QualityPolicy != nil {
		if r.Version < 2 {
			return fmt.Errorf("quality policy requires rules version 2")
		}
		if err := r.QualityPolicy.Validate(r.Resolution); err != nil {
			return err
		}
	}
	if len(r.Required) > 10 || len(r.Excluded) > 10 || len(r.Fansub) > 100 {
		return fmt.Errorf("too many filter conditions")
	}
	for _, word := range append(append([]string{}, r.Required...), r.Excluded...) {
		if strings.TrimSpace(word) == "" || len(word) > 100 {
			return fmt.Errorf("invalid filter condition")
		}
	}
	return nil
}

func Evaluate(raw string, item rss.RSSItem) Decision {
	if raw == "" {
		return Decision{"match", "legacy_subscription"}
	}
	rules, err := Decode(raw)
	if err != nil {
		return Decision{"review", "invalid_discovery_rules"}
	}
	return rules.Match(item)
}

// A scoped Mikan preview must not confer its identity evidence on a later-added feed.
func EvaluateForFeed(raw string, item rss.RSSItem, feedURL string, offset int) Decision {
	if raw == "" {
		return Decision{"match", "legacy_subscription"}
	}
	rules, err := Decode(raw)
	if err != nil {
		return Decision{"review", "invalid_discovery_rules"}
	}
	if rules.EpisodeOffset != offset {
		return Decision{"review", "episode_mapping_changed_reprepare_required"}
	}
	rules.ScopedFeed = rules.ScopedFeed && rules.ScopedFeedURL == feedURL
	return rules.Match(item)
}

func (r Rules) Match(item rss.RSSItem) Decision {
	title := strings.ToLower(item.Title)
	if batchPattern.MatchString(title) {
		return Decision{"reject", "batch_resource"}
	}
	if specialPattern.MatchString(title) {
		return Decision{"reject", "special_or_movie"}
	}
	matched := false
	ambiguousSeason := false
	for _, alias := range r.Titles {
		if containsTitle(title, strings.ToLower(alias)) {
			matched = true
			end := strings.Index(title, strings.ToLower(alias)) + len(alias)
			if seasonSuffixPattern.MatchString(title[end:]) {
				ambiguousSeason = true
			}
		}
	}
	if !matched {
		return Decision{"reject", "title_mismatch"}
	}
	season := ExplicitSeason(item.Title)
	if season == 0 && ambiguousSeason {
		return Decision{"review", "ambiguous_season_suffix"}
	}
	if season != 0 && season != r.Season {
		return Decision{"reject", "season_mismatch"}
	}
	if season == 0 && r.Season > 1 && !r.ScopedFeed && r.EpisodeOffset == 0 {
		return Decision{"review", "season_not_identified"}
	}
	episode := item.Episode - r.EpisodeOffset
	if episode <= 0 {
		return Decision{"review", "episode_not_mapped"}
	}
	if r.TotalEpisodes > 0 && episode > r.TotalEpisodes {
		return Decision{"reject", "episode_out_of_range"}
	}
	if r.TrustedOnly && !item.Trusted {
		return Decision{"reject", "uploader_not_marked_trusted"}
	}
	if r.Fansub != "" && !matchesFansub(r.Fansub, item.Fansub) {
		return Decision{"reject", "fansub_mismatch"}
	}
	for _, word := range r.Excluded {
		if strings.Contains(title, strings.ToLower(word)) {
			return Decision{"reject", "excluded_keyword: " + word}
		}
	}
	for _, word := range r.Required {
		if !strings.Contains(title, strings.ToLower(word)) {
			return Decision{"reject", "required_keyword_missing: " + word}
		}
	}
	if r.QualityPolicy != nil {
		if decision := r.QualityPolicy.Match(title); decision.Action != "match" {
			return decision
		}
	}
	if r.Resolution != 0 {
		if !resolutionPatterns[r.Resolution].MatchString(title) {
			return Decision{"review", "required_resolution_not_identified"}
		}
	}
	if r.Language != "any" && !hasLanguage(title, item.CategoryID, r.Language) {
		return Decision{"review", "required_language_not_identified"}
	}
	return Decision{"match", "confirmed_rules_match"}
}

// Latin aliases use word boundaries so a short title cannot match inside another word.
func containsTitle(title, alias string) bool {
	for start := 0; start < len(title); {
		index := strings.Index(title[start:], alias)
		if index < 0 {
			return false
		}
		index += start
		before, after := []rune(title[:index]), []rune(title[index+len(alias):])
		first, last := []rune(alias)[0], []rune(alias)[len([]rune(alias))-1]
		latin := func(r rune) bool { return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }
		if !(latin(first) && len(before) > 0 && latin(before[len(before)-1])) && !(latin(last) && len(after) > 0 && latin(after[0])) {
			return true
		}
		start = index + len(alias)
	}
	return false
}

func hasLanguage(title, category, language string) bool {
	pattern := languagePatterns[language]
	return (language == "en" && category == "1_2") || (pattern != nil && pattern.MatchString(title))
}

func matchesFansub(required, actual string) bool {
	if strings.EqualFold(strings.TrimSpace(required), strings.TrimSpace(actual)) {
		return true
	}
	for _, group := range strings.FieldsFunc(actual, func(r rune) bool { return r == '&' || r == '+' || r == '、' }) {
		if strings.EqualFold(strings.TrimSpace(required), strings.TrimSpace(group)) {
			return true
		}
	}
	return false
}
