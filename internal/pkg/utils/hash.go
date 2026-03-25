package utils

import (
	"regexp"
	"strings"

	ext "github.com/mmcdole/gofeed/extensions"
)

// ExtractInfoHashFromTorrentURL 从 .torrent URL 提取 40 位十六进制 hash（如 mikan 链接）
// 支持 URL 格式: /xxxx.torrent 或 /xxxx.torrent?...
func ExtractInfoHashFromTorrentURL(torrentURL string) string {
	re := regexp.MustCompile(`(?i)/([a-f0-9]{40})\.torrent(?:$|\?)`)
	m := re.FindStringSubmatch(torrentURL)
	if len(m) == 2 {
		return strings.ToLower(m[1])
	}
	return ""
}

// ExtractHashFromURL 从 URL 中提取 hash
// 支持 magnet link 的 btih (BitTorrent Info Hash)
func ExtractHashFromURL(url string) string {
	// 处理 magnet link
	if strings.HasPrefix(strings.ToLower(url), "magnet:") {
		// 查找 btih (BitTorrent Info Hash)
		url = strings.ToLower(url)
		if idx := strings.Index(url, "btih:"); idx != -1 {
			hash := url[idx+5:]
			// 截取到下一个 & 或字符串结束
			if endIdx := strings.Index(hash, "&"); endIdx != -1 {
				hash = hash[:endIdx]
			}
			// hash 应该是 40 个十六进制字符
			if len(hash) == 40 {
				return strings.ToLower(hash)
			}
			// 或者是 32 个 base32 字符（需要转换，暂时跳过）
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

// HashPrefix 返回 hash 的前 8 个字符，用于日志记录
// 如果 hash 长度小于等于 8，则返回完整 hash
func HashPrefix(hash string) string {
	hash = strings.TrimSpace(hash)
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}
