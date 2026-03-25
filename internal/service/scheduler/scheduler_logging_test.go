package scheduler

import (
	"testing"

	"github.com/WormW/auto-rss/internal/pkg/utils"
)

func TestHashPrefix(t *testing.T) {
	if got := utils.HashPrefix("1234567890abcdef"); got != "12345678" {
		t.Fatalf("HashPrefix truncation = %q, want %q", got, "12345678")
	}
	if got := utils.HashPrefix("abc"); got != "abc" {
		t.Fatalf("HashPrefix short hash = %q, want %q", got, "abc")
	}
}
