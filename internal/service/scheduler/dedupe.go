package scheduler

import (
	"regexp"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/service/rss"
)

const autoCollectReplaceCooldown = 6 * time.Hour

var versionRe = regexp.MustCompile(`(?i)(?:^|\s|\-|\[|\()v(\d+)(?:$|\s|\]|\)|\.)`)

func parseTitleVersion(title string) int {
	// Default version is 1.
	// Matches patterns like " v2 ", "- v2", "[v2]", "(v3)".
	m := versionRe.FindStringSubmatch(title)
	if len(m) < 2 {
		return 1
	}
	v := 0
	for i := 0; i < len(m[1]); i++ {
		c := m[1][i]
		if c < '0' || c > '9' {
			return 1
		}
		v = v*10 + int(c-'0')
		if v > 100 {
			break
		}
	}
	if v <= 0 {
		return 1
	}
	return v
}

func shouldReplaceExistingEpisodeAuto(existing *model.Download, item rss.RSSItem, now time.Time) (bool, string) {
	if existing == nil {
		return true, "no_existing"
	}

	// If the hash is exactly the same, it is a true duplicate.
	if existing.TorrentHash != "" && item.TorrentHash != "" && strings.EqualFold(existing.TorrentHash, item.TorrentHash) {
		return false, "same_hash"
	}

	oldV := parseTitleVersion(existing.Title)
	newV := parseTitleVersion(item.Title)
	if newV > oldV {
		return true, "newer_version"
	}

	// Not a newer version; avoid auto scheduler repeatedly replacing tasks due to unstable hashes.
	if !existing.CreatedAt.IsZero() && now.Sub(existing.CreatedAt) < autoCollectReplaceCooldown {
		return false, "cooldown_not_newer"
	}

	return false, "not_newer"
}
