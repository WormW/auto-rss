package downloader

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
)

func TestShouldSkipReconcileByGracePeriod(t *testing.T) {
	now := time.Date(2026, 2, 25, 14, 30, 0, 0, time.FixedZone("CST", 8*3600))
	withinGrace := model.Download{UpdatedAt: now.Add(-5 * time.Minute)}
	outOfGrace := model.Download{UpdatedAt: now.Add(-15 * time.Minute)}

	if !shouldSkipReconcileByGracePeriod(&withinGrace, now) {
		t.Fatalf("expected within-grace download to be skipped")
	}
	if shouldSkipReconcileByGracePeriod(&outOfGrace, now) {
		t.Fatalf("expected out-of-grace download not to be skipped")
	}
}
