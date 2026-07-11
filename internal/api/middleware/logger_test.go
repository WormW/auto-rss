package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldSkipRequestLog(t *testing.T) {
	tests := map[string]bool{
		"/health":               true,
		"/api/v1/health":        true,
		"/ready":                true,
		"/live":                 true,
		"/api/v1/logs":          true,
		"/api/v1/logs/clear":    true,
		"/api/v1/subscriptions": false,
	}

	for path, expected := range tests {
		require.Equal(t, expected, shouldSkipRequestLog(path), path)
	}
}
