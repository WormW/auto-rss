package model

import "testing"

func TestSubscriptionEpisodeProgress(t *testing.T) {
	tests := []struct {
		name          string
		subscription  Subscription
		wantCurrent   int
		wantLatest    int
		wantCompleted bool
	}{
		{
			name: "偏移订阅尚差一集",
			subscription: Subscription{
				EpisodeOffset:  170,
				TotalEpisodes:  52,
				CurrentEpisode: 221,
				LatestEpisode:  222,
			},
			wantCurrent:   51,
			wantLatest:    52,
			wantCompleted: false,
		},
		{
			name: "偏移订阅达到最后一集",
			subscription: Subscription{
				EpisodeOffset:  170,
				TotalEpisodes:  52,
				CurrentEpisode: 222,
				LatestEpisode:  222,
			},
			wantCurrent:   52,
			wantLatest:    52,
			wantCompleted: true,
		},
		{
			name: "无偏移保持原有行为",
			subscription: Subscription{
				TotalEpisodes:  12,
				CurrentEpisode: 12,
				LatestEpisode:  12,
			},
			wantCurrent:   12,
			wantLatest:    12,
			wantCompleted: true,
		},
		{
			name: "负偏移按无偏移处理",
			subscription: Subscription{
				EpisodeOffset:  -5,
				TotalEpisodes:  12,
				CurrentEpisode: 10,
				LatestEpisode:  11,
			},
			wantCurrent:   10,
			wantLatest:    11,
			wantCompleted: false,
		},
		{
			name: "原始集号小于偏移时进度为零",
			subscription: Subscription{
				EpisodeOffset:  170,
				TotalEpisodes:  52,
				CurrentEpisode: 169,
				LatestEpisode:  170,
			},
			wantCurrent:   0,
			wantLatest:    0,
			wantCompleted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.subscription.RelativeCurrentEpisode(); got != tt.wantCurrent {
				t.Fatalf("RelativeCurrentEpisode() = %d, want %d", got, tt.wantCurrent)
			}
			if got := tt.subscription.RelativeLatestEpisode(); got != tt.wantLatest {
				t.Fatalf("RelativeLatestEpisode() = %d, want %d", got, tt.wantLatest)
			}
			if got := tt.subscription.IsCompleted(); got != tt.wantCompleted {
				t.Fatalf("IsCompleted() = %v, want %v", got, tt.wantCompleted)
			}
		})
	}
}
