package main

import (
	"strings"
	"testing"

	"github.com/WormW/auto-rss/internal/service/recovery"
)

func TestParseModeAcceptsDryRun(t *testing.T) {
	dryRun, err := parseMode([]string{"dry-run"})
	if err != nil {
		t.Fatalf("expected dry-run mode to parse: %v", err)
	}
	if !dryRun {
		t.Fatal("expected dry-run mode to enable dry-run behavior")
	}
}

func TestParseModeAcceptsApply(t *testing.T) {
	dryRun, err := parseMode([]string{"apply"})
	if err != nil {
		t.Fatalf("expected apply mode to parse: %v", err)
	}
	if dryRun {
		t.Fatal("expected apply mode to disable dry-run behavior")
	}
}

func TestApplyModeRequiresRecoveryApplyGate(t *testing.T) {
	got := recovery.ErrApplyDisabled.Error()
	if got == "" ||
		!strings.Contains(got, "AUTO_RSS_ENABLE_RECOVERY_APPLY=true") ||
		!strings.Contains(got, "explicit human approval") {
		t.Fatalf("expected shared apply gate error to name env var and human approval, got %q", got)
	}
}

func TestParseModeRejectsUnsupportedMode(t *testing.T) {
	if _, err := parseMode([]string{"dr-run"}); err == nil {
		t.Fatal("expected unsupported mode to fail")
	}
}

func TestParseModeRejectsMissingMode(t *testing.T) {
	if _, err := parseMode(nil); err == nil {
		t.Fatal("expected missing mode to fail")
	}
}
