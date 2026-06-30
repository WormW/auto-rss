package scheduler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/rss"
)

const (
	minTorrentSizeBytesConfigKey = "min_torrent_size_bytes"
	defaultMinTorrentSizeBytes   = int64(50 * 1024 * 1024)
)

// MinTorrentSizeBytes returns the configured small-torrent threshold.
func MinTorrentSizeBytes(configRepo repository.ConfigRepository) int64 {
	return minTorrentSizeBytes(configRepo)
}

func minTorrentSizeBytes(configRepo repository.ConfigRepository) int64 {
	if configRepo == nil {
		return defaultMinTorrentSizeBytes
	}

	cfg, err := configRepo.Get(minTorrentSizeBytesConfigKey)
	if err != nil || cfg == nil {
		return defaultMinTorrentSizeBytes
	}

	value := strings.TrimSpace(cfg.Value)
	if value == "" {
		return defaultMinTorrentSizeBytes
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return defaultMinTorrentSizeBytes
	}

	return parsed
}

// SmallTorrentSkipMessage describes why a known-size RSS item was skipped.
func SmallTorrentSkipMessage(item *rss.RSSItem, minSizeBytes int64) string {
	return smallTorrentSkipMessage(item, minSizeBytes)
}

func smallTorrentSkipMessage(item *rss.RSSItem, minSizeBytes int64) string {
	return fmt.Sprintf("skipped because torrent payload size %d bytes is below minimum %d bytes", item.SizeBytes, minSizeBytes)
}

// ShouldSkipSmallTorrent reports whether a known-size RSS item is below the configured threshold.
func ShouldSkipSmallTorrent(item *rss.RSSItem, minSizeBytes int64) bool {
	return shouldSkipSmallTorrent(item, minSizeBytes)
}

func shouldSkipSmallTorrent(item *rss.RSSItem, minSizeBytes int64) bool {
	if item == nil || minSizeBytes <= 0 {
		return false
	}
	return item.SizeBytes > 0 && item.SizeBytes < minSizeBytes
}
