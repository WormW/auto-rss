package main

import "testing"

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
