package utils

import (
	"regexp"
	"strings"

	ext "github.com/mmcdole/gofeed/extensions"
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

// ExtractInfoHashFromExtensions 从 RSS 扩展字段中提取 info-hash
func ExtractInfoHashFromExtensions(extensions ext.Extensions) string {
	if len(extensions) == 0 {
		return ""
	}

	for ns, fields := range extensions {
		for key, values := range fields {
			if !strings.EqualFold(ns, "nyaa") || !strings.EqualFold(key, "infoHash") {
				continue
			}
			for _, ext := range values {
				v := strings.TrimSpace(ext.Value)
				if len(v) == 40 {
					if ok, _ := regexp.MatchString(`(?i)^[a-f0-9]{40}$`, v); ok {
						return strings.ToLower(v)
					}
				}
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
