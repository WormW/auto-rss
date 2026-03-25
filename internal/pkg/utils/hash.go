package utils

import (
	"regexp"
	"strings"
)

// ExtractInfoHashFromTorrentURL 从 .torrent URL 提取 40 位十六进制 hash
func ExtractInfoHashFromTorrentURL(torrentURL string) string {
	re := regexp.MustCompile(`(?i)/([a-f0-9]{40})\.torrent(?:$|\?)`)
	m := re.FindStringSubmatch(torrentURL)
	if len(m) == 2 {
		return strings.ToLower(m[1])
	}
	return ""
}

// ExtractHashFromURL 从 URL/磁链 提取 hash
func ExtractHashFromURL(url string) string {
	if strings.HasPrefix(strings.ToLower(url), "magnet:") {
		url = strings.ToLower(url)
		if idx := strings.Index(url, "btih:"); idx != -1 {
			hash := url[idx+5:]
			if endIdx := strings.Index(hash, "&"); endIdx != -1 {
				hash = hash[:endIdx]
			}
			if len(hash) == 40 {
				return strings.ToLower(hash)
			}
		}
	}
	return ""
}

// HashPrefix 返回 hash 前 8 位
func HashPrefix(hash string) string {
	hash = strings.TrimSpace(hash)
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}
