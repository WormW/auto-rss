package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractInfoHashFromTorrentURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "标准的mikan链接",
			url:      "https://mikanime.tv/Download/20260218/4f9c570f6b9ff86278629d352ba8272633a18c3b.torrent",
			expected: "4f9c570f6b9ff86278629d352ba8272633a18c3b",
		},
		{
			name:     "带查询参数的链接",
			url:      "https://example.com/download/ABCDEF0123456789ABCDEF0123456789ABCDEF01.torrent?token=abc123",
			expected: "abcdef0123456789abcdef0123456789abcdef01",
		},
		{
			name:     "大写hash自动转小写",
			url:      "https://example.com/ABCDEF0123456789ABCDEF0123456789ABCDEF01.torrent",
			expected: "abcdef0123456789abcdef0123456789abcdef01",
		},
		{
			name:     "无效的hash长度",
			url:      "https://example.com/abcd1234.torrent",
			expected: "",
		},
		{
			name:     "非torrent链接",
			url:      "https://example.com/download/file.zip",
			expected: "",
		},
		{
			name:     "空字符串",
			url:      "",
			expected: "",
		},
		{
			name:     "hash包含非法字符",
			url:      "https://example.com/ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCD.torrent",
			expected: "",
		},
		{
			name:     "只有39个字符",
			url:      "https://example.com/abcdef0123456789abcdef0123456789abcde.torrent",
			expected: "",
		},
		{
			name:     "路径中有多个torrent",
			url:      "https://example.com/abcdef0123456789abcdef0123456789abcdef01.torrent/extra",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractInfoHashFromTorrentURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractHashFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "标准magnet链接",
			url:      "magnet:?xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01&dn=test",
			expected: "abcdef0123456789abcdef0123456789abcdef01",
		},
		{
			name:     "magnet链接大写转小写",
			url:      "magnet:?xt=urn:btih:ABCDEF0123456789ABCDEF0123456789ABCDEF01",
			expected: "abcdef0123456789abcdef0123456789abcdef01",
		},
		{
			name:     "magnet链接只有btih",
			url:      "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678",
			expected: "1234567890abcdef1234567890abcdef12345678",
		},
		{
			name:     "非magnet链接",
			url:      "https://example.com/download/file.torrent",
			expected: "",
		},
		{
			name:     "magnet链接无btih",
			url:      "magnet:?xt=urn:other:abcdef123456&dn=test",
			expected: "",
		},
		{
			name:     "空字符串",
			url:      "",
			expected: "",
		},
		{
			name:     "magnet链接hash长度不足",
			url:      "magnet:?xt=urn:btih:abc123",
			expected: "",
		},
		{
			name:     "magnet链接btih在末尾",
			url:      "magnet:?dn=test&xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01",
			expected: "abcdef0123456789abcdef0123456789abcdef01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractHashFromURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHashPrefix(t *testing.T) {
	tests := []struct {
		name     string
		hash     string
		expected string
	}{
		{
			name:     "标准40位hash",
			hash:     "abcdef0123456789abcdef0123456789abcdef01",
			expected: "abcdef01",
		},
		{
			name:     "短hash",
			hash:     "abc123",
			expected: "abc123",
		},
		{
			name:     "正好8位hash",
			hash:     "abcdef12",
			expected: "abcdef12",
		},
		{
			name:     "包含空格的hash",
			hash:     "  abcdef0123456789  ",
			expected: "abcdef01",
		},
		{
			name:     "空字符串",
			hash:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HashPrefix(tt.hash)
			assert.Equal(t, tt.expected, result)
		})
	}
}
