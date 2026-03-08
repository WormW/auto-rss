package scheduler

import "testing"

func TestHashPrefix(t *testing.T) {
	if got := hashPrefix("1234567890abcdef"); got != "12345678" {
		t.Fatalf("hashPrefix truncation = %q, want %q", got, "12345678")
	}
	if got := hashPrefix("abc"); got != "abc" {
		t.Fatalf("hashPrefix short hash = %q, want %q", got, "abc")
	}
}
