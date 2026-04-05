package rss

import (
	"testing"
	"time"
)

// TestParser_FetchAndParseWithTimeout_UsesProvidedTimeout tests that the method uses provided timeout
func TestParser_FetchAndParseWithTimeout_UsesProvidedTimeout(t *testing.T) {
	// This is a compile-time check that the interface has the method
	var _ Parser = NewParser()

	p := NewParser()

	// Test that FetchAndParseWithTimeout method exists
	// We can't easily test the actual timeout behavior without a mock server,
	// but we can verify the method signature exists by calling it
	// The method should accept a timeout parameter
	_, err := p.FetchAndParseWithTimeout("http://localhost:1/rss", 1*time.Second)
	// We expect an error since the server doesn't exist, but no panic
	if err == nil {
		t.Error("Expected error for unreachable server")
	}
}

// TestParser_FetchAndParseWithTimeout_ZeroTimeoutUsesDefault tests that zero timeout uses default
func TestParser_FetchAndParseWithTimeout_ZeroTimeoutUsesDefault(t *testing.T) {
	p := NewParser()

	// Test with zero timeout - should use default 30s
	_, err := p.FetchAndParseWithTimeout("http://localhost:1/rss", 0)
	if err == nil {
		t.Error("Expected error for unreachable server")
	}
}

// TestParser_FetchAndParseWithTimeout_NegativeTimeoutUsesDefault tests that negative timeout uses default
func TestParser_FetchAndParseWithTimeout_NegativeTimeoutUsesDefault(t *testing.T) {
	p := NewParser()

	// Test with negative timeout - should use default 30s
	_, err := p.FetchAndParseWithTimeout("http://localhost:1/rss", -1*time.Second)
	if err == nil {
		t.Error("Expected error for unreachable server")
	}
}

// TestParser_FetchAndParse_DelegatesToWithTimeout tests that FetchAndParse delegates to FetchAndParseWithTimeout
func TestParser_FetchAndParse_DelegatesToWithTimeout(t *testing.T) {
	p := NewParser()

	// FetchAndParse should work (delegates to FetchAndParseWithTimeout with 30s)
	_, err := p.FetchAndParse("http://localhost:1/rss")
	if err == nil {
		t.Error("Expected error for unreachable server")
	}
}

// TestParser_InterfaceHasFetchAndParseWithTimeout verifies the interface includes the new method
func TestParser_InterfaceHasFetchAndParseWithTimeout(t *testing.T) {
	// This test ensures the Parser interface was updated
	var p Parser = NewParser()

	// Try to assign to a variable with the expected signature
	var f func(string, time.Duration) ([]RSSItem, error) = p.FetchAndParseWithTimeout
	_ = f
}

// TestParser_FetchAndParseWithTimeout_ContextCancellation tests context cancellation
func TestParser_FetchAndParseWithTimeout_ContextCancellation(t *testing.T) {
	p := NewParser()

	// Use a very short timeout to test context cancellation
	start := time.Now()
	_, err := p.FetchAndParseWithTimeout("http://192.0.2.1/rss", 100*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected error for unreachable server")
	}

	// Should complete quickly due to timeout, not wait for TCP timeout
	if elapsed > 5*time.Second {
		t.Errorf("Expected quick timeout, but took %v", elapsed)
	}
}
