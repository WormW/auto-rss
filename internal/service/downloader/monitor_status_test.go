package downloader

import "testing"

func TestMapQBStateToStatus(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  string
	}{
		{name: "real downloading", state: "downloading", want: "downloading"},
		{name: "forced downloading", state: "forcedDL", want: "downloading"},
		{name: "stalled downloading", state: "stalledDL", want: "stalled"},
		{name: "paused downloading", state: "pausedDL", want: "stalled"},
		{name: "queued downloading", state: "queuedDL", want: "stalled"},
		{name: "meta downloading", state: "metaDL", want: "stalled"},
		{name: "checking downloading", state: "checkingDL", want: "stalled"},
		{name: "uploading completed", state: "uploading", want: "completed"},
		{name: "stalled uploading completed", state: "stalledUP", want: "completed"},
		{name: "error failed", state: "error", want: "failed"},
		{name: "missing files failed", state: "missingFiles", want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapQBStateToStatus(tt.state)
			if got != tt.want {
				t.Fatalf("mapQBStateToStatus(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}
