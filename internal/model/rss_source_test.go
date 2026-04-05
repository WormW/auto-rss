package model

import (
	"testing"
	"time"
)

// TestRSSSource_TimeoutField tests that RSSSource has Timeout field with correct type and tags
func TestRSSSource_TimeoutField(t *testing.T) {
	source := RSSSource{
		ID:      1,
		Name:    "Test Source",
		BaseURL: "https://example.com/rss",
		Timeout: 45 * time.Second,
	}

	// Test that Timeout field exists and is time.Duration type
	var _ time.Duration = source.Timeout

	// Test zero value
	zeroSource := RSSSource{}
	if zeroSource.Timeout != 0 {
		t.Errorf("Expected zero value for Timeout, got %v", zeroSource.Timeout)
	}
}

// TestRSSSource_DefaultTimeout tests the DefaultRSSTimeout helper function
func TestRSSSource_DefaultTimeout(t *testing.T) {
	defaultTimeout := DefaultRSSTimeout()
	expected := 30 * time.Second

	if defaultTimeout != expected {
		t.Errorf("DefaultRSSTimeout() = %v, want %v", defaultTimeout, expected)
	}
}

// TestRSSSource_TableName tests the table name
func TestRSSSource_TableName(t *testing.T) {
	source := RSSSource{}
	if source.TableName() != "rss_sources" {
		t.Errorf("TableName() = %v, want rss_sources", source.TableName())
	}
}
