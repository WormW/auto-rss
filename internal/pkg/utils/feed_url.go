package utils

import (
	"net"
	"net/url"
	"strings"
)

func NormalizeFeedURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	} else {
		parsed.Host = host
	}
	parsed.Fragment = ""
	parsed.RawQuery = parsed.Query().Encode()
	return parsed.String()
}

// IsMikanMyBangumiCollectionURL reports whether raw is Mikan's account-wide RSS
// collection feed. It is not scoped to a single anime and must never be used as
// a subscription source, otherwise every item can be assigned to that subscription.
func IsMikanMyBangumiCollectionURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimRight(parsed.Path, "/"), "/RSS/MyBangumi")
}
