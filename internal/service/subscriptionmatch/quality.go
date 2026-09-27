package subscriptionmatch

import (
	"fmt"
	"slices"

	"github.com/WormW/auto-rss/internal/service/rss"
)

// QualityPolicy separates exclusions from preference and fallback behavior.
// Nil policies on existing rules retain their original behavior.
type QualityPolicy struct {
	ExcludedResolutions []int  `json:"excluded_resolutions" jsonschema:"Hard exclusions. Supported values: 480, 720, 1080, 2160."`
	PreferredResolution int    `json:"preferred_resolution" jsonschema:"Prefer this resolution among items in the same RSS fetch. Zero disables ranking."`
	Fallback            string `json:"fallback" jsonschema:"wait skips other resolutions pending review; allow accepts identified nonexcluded resolutions. No automatic replacement of existing downloads."`
}

func DefaultQualityPolicy() *QualityPolicy {
	return &QualityPolicy{ExcludedResolutions: []int{720}, PreferredResolution: 1080, Fallback: "wait"}
}

func (p QualityPolicy) Validate(required int) error {
	if p.PreferredResolution != 0 && resolutionPatterns[p.PreferredResolution] == nil {
		return fmt.Errorf("unsupported preferred resolution")
	}
	if p.Fallback != "wait" && p.Fallback != "allow" {
		return fmt.Errorf("quality fallback must be wait or allow")
	}
	if p.Fallback == "wait" && p.PreferredResolution == 0 {
		return fmt.Errorf("wait requires a preferred resolution")
	}
	seen := map[int]bool{}
	for _, resolution := range p.ExcludedResolutions {
		if resolutionPatterns[resolution] == nil || seen[resolution] {
			return fmt.Errorf("invalid or duplicate excluded resolution")
		}
		seen[resolution] = true
	}
	if seen[required] || seen[p.PreferredResolution] || (required != 0 && p.PreferredResolution != 0 && required != p.PreferredResolution) {
		return fmt.Errorf("conflicting required, preferred, or excluded resolutions")
	}
	return nil
}

func (p QualityPolicy) Match(title string) Decision {
	resolution, count := 0, 0
	for size, pattern := range resolutionPatterns {
		if !pattern.MatchString(title) {
			continue
		}
		if slices.Contains(p.ExcludedResolutions, size) {
			return Decision{"reject", "excluded_resolution"}
		}
		resolution, count = size, count+1
	}
	if count == 0 {
		return Decision{"review", "resolution_not_identified"}
	}
	if count > 1 {
		return Decision{"review", "ambiguous_resolution"}
	}
	if p.Fallback == "wait" && resolution != p.PreferredResolution {
		return Decision{"review", "waiting_for_preferred_resolution"}
	}
	return Decision{"match", "quality_policy_match"}
}

// Prioritize returns a stable, independent ordering without modifying cached RSS
// data. The caller still applies all matching rules before selecting a download.
func (r Rules) Prioritize(items []rss.RSSItem) []rss.RSSItem {
	if r.QualityPolicy == nil || r.QualityPolicy.PreferredResolution == 0 {
		return items
	}
	pattern := resolutionPatterns[r.QualityPolicy.PreferredResolution]
	if pattern == nil {
		return items
	}
	ordered := make([]rss.RSSItem, 0, len(items))
	for _, preferred := range []bool{true, false} {
		for _, item := range items {
			if pattern.MatchString(item.Title) == preferred {
				ordered = append(ordered, item)
			}
		}
	}
	return ordered
}

func Prioritize(raw string, items []rss.RSSItem) []rss.RSSItem {
	rules, err := Decode(raw)
	if err != nil {
		return items
	}
	return rules.Prioritize(items)
}
