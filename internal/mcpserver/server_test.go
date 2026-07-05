package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WormW/auto-rss/internal/config"
)

func TestMCPAuthAndOrigin(t *testing.T) {
	server := &Server{
		cfg: &config.Config{
			MCPToken:          "secret-token",
			MCPAllowedOrigins: []string{"http://localhost:7892"},
		},
	}

	unauthorized := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if server.authorized(unauthorized) {
		t.Fatal("request without bearer token was authorized")
	}

	authorized := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	authorized.Header.Set("Authorization", "Bearer secret-token")
	if !server.authorized(authorized) {
		t.Fatal("request with matching bearer token was not authorized")
	}

	allowedOrigin := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	allowedOrigin.Header.Set("Origin", "http://localhost:7892")
	if !server.allowedOrigin(allowedOrigin) {
		t.Fatal("configured origin was not allowed")
	}

	blockedOrigin := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	blockedOrigin.Header.Set("Origin", "http://evil.example")
	if server.allowedOrigin(blockedOrigin) {
		t.Fatal("unconfigured origin was allowed")
	}
}

func TestMCPWriteCORSHeaders(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	req.Header.Set("Origin", "http://localhost:7892")
	rec := httptest.NewRecorder()

	server.writeCORSHeaders(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:7892" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want configured request origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("Access-Control-Allow-Methods was not set")
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}
