package episode

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type serviceFixture struct {
	db      *gorm.DB
	repo    repository.EpisodeRepository
	service *Service
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	repo := repository.NewEpisodeRepository(db)
	return &serviceFixture{db: db, repo: repo, service: NewService(repo)}
}

func TestEvaluateRSSItemCreatesCandidateForDifferentDownloadedResource(t *testing.T) {
	fx := newServiceFixture(t)
	sub := model.Subscription{ID: 1, Name: "show"}
	require.NoError(t, fx.db.Create(&sub).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID:    sub.ID,
		Episode:           4,
		Status:            model.EpisodeStatusDownloaded,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveTorrentHash: "old",
		ActiveTorrentURL:  "https://x/old",
		ActiveTitle:       "old title",
	}
	require.NoError(t, fx.db.Create(&ledger).Error)
	pubTime := time.Date(2026, time.July, 12, 1, 2, 3, 0, time.UTC)

	decision, err := fx.service.EvaluateRSSItem(context.Background(), &sub, RSSResource{
		OriginalEpisode: 4,
		Resource:        model.EpisodeResource{Hash: "NEW", URL: "https://x/new", Title: "new title"},
		Fansub:          "Group",
		Language:        "CHS",
		PubTime:         pubTime,
		SourceRSSURL:    "https://rss/feed.xml",
	}, false)
	require.NoError(t, err)
	assert.Equal(t, DecisionCandidate, decision.Action)
	assert.Equal(t, ledger.ID, decision.EpisodeID)
	assert.NotZero(t, decision.CandidateID)

	candidates, err := fx.repo.ListCandidates(ledger.ID)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "hash:new", candidates[0].ResourceKey)
	assert.Equal(t, "NEW", candidates[0].TorrentHash)
	assert.Equal(t, "https://x/new", candidates[0].TorrentURL)
	assert.Equal(t, "new title", candidates[0].Title)
	assert.Equal(t, "Group", candidates[0].Fansub)
	assert.Equal(t, "CHS", candidates[0].Language)
	assert.Equal(t, "https://rss/feed.xml", candidates[0].SourceRSSURL)
	assert.Equal(t, model.CandidateStatusPending, candidates[0].Status)
	require.NotNil(t, candidates[0].PubTime)
	assert.Equal(t, pubTime, *candidates[0].PubTime)
}

func TestRefreshSubscriptionProgressUsesContinuousOwnedEpisodesAndOffset(t *testing.T) {
	fx := newServiceFixture(t)
	sub := model.Subscription{ID: 1, Name: "offset", EpisodeOffset: 170, TotalEpisodes: 4}
	require.NoError(t, fx.db.Create(&sub).Error)
	require.NoError(t, fx.db.Create(&[]model.SubscriptionEpisode{
		{SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: "test"},
		{SubscriptionID: sub.ID, Episode: 2, Status: model.EpisodeStatusMarkedDownloaded, StatusSource: "test"},
		{SubscriptionID: sub.ID, Episode: 4, Status: model.EpisodeStatusDownloaded, StatusSource: "test"},
	}).Error)

	require.NoError(t, fx.service.RefreshSubscriptionProgress(sub.ID))
	require.NoError(t, fx.db.First(&sub, sub.ID).Error)
	assert.Equal(t, 172, sub.CurrentEpisode)
	assert.Equal(t, 174, sub.LatestEpisode)
	assert.Nil(t, sub.CompletedAt)
}

func TestResourceKeyNormalizesHashBeforeURL(t *testing.T) {
	assert.Equal(t, "hash:abcdef", ResourceKey(model.EpisodeResource{
		Hash: " ABCDEF ",
		URL:  "https://x/e01",
	}))
	assert.Equal(t, "url:https://x/e01", ResourceKey(model.EpisodeResource{URL: "  https://x/e01  "}))
	assert.Empty(t, ResourceKey(model.EpisodeResource{}))
}

func TestEvaluateRSSItemDecisionMatrix(t *testing.T) {
	tests := []struct {
		name           string
		sub            model.Subscription
		ledger         *model.SubscriptionEpisode
		item           RSSResource
		baseline       bool
		wantAction     string
		wantReason     string
		wantCandidates int
	}{
		{
			name:       "non-positive relative episode skips",
			sub:        model.Subscription{ID: 1, EpisodeOffset: 10},
			item:       RSSResource{OriginalEpisode: 10, Resource: model.EpisodeResource{Hash: "a"}},
			wantAction: DecisionSkip,
			wantReason: "non_positive_relative_episode",
		},
		{
			name:       "missing resource identity skips",
			sub:        model.Subscription{ID: 1},
			item:       RSSResource{OriginalEpisode: 1},
			wantAction: DecisionSkip,
			wantReason: "resource_identity_missing",
		},
		{
			name:       "ignored stays ignored",
			sub:        model.Subscription{ID: 1},
			ledger:     ledgerForMatrix(model.EpisodeStatusIgnored, "", ""),
			item:       RSSResource{OriginalEpisode: 1, Resource: model.EpisodeResource{Hash: "a"}},
			wantAction: DecisionIgnored,
			wantReason: "episode_ignored",
		},
		{
			name:       "missing claims download",
			sub:        model.Subscription{ID: 1},
			ledger:     ledgerForMatrix(model.EpisodeStatusMissing, "", ""),
			item:       RSSResource{OriginalEpisode: 1, Resource: model.EpisodeResource{Hash: "a"}},
			wantAction: DecisionDownload,
			wantReason: "episode_missing",
		},
		{
			name:       "baseline only observes missing",
			sub:        model.Subscription{ID: 1},
			item:       RSSResource{OriginalEpisode: 1, Resource: model.EpisodeResource{Hash: "a"}},
			baseline:   true,
			wantAction: DecisionBaseline,
			wantReason: "baseline_observed",
		},
		{
			name:       "downloading same hash skips case insensitive",
			sub:        model.Subscription{ID: 1},
			ledger:     ledgerForMatrix(model.EpisodeStatusDownloading, "ABC", "https://old"),
			item:       RSSResource{OriginalEpisode: 1, Resource: model.EpisodeResource{Hash: "abc", URL: "https://new"}},
			wantAction: DecisionSkip,
			wantReason: "resource_already_known",
		},
		{
			name:           "downloaded different hash creates candidate",
			sub:            model.Subscription{ID: 1},
			ledger:         ledgerForMatrix(model.EpisodeStatusDownloaded, "old", ""),
			item:           RSSResource{OriginalEpisode: 1, Resource: model.EpisodeResource{Hash: "new"}},
			wantAction:     DecisionCandidate,
			wantReason:     "different_resource",
			wantCandidates: 1,
		},
		{
			name:       "marked downloaded same url skips",
			sub:        model.Subscription{ID: 1},
			ledger:     ledgerForMatrix(model.EpisodeStatusMarkedDownloaded, "", " https://x/e01 "),
			item:       RSSResource{OriginalEpisode: 1, Resource: model.EpisodeResource{URL: "https://x/e01"}},
			wantAction: DecisionSkip,
			wantReason: "resource_already_known",
		},
		{
			name:           "empty current identity creates candidate",
			sub:            model.Subscription{ID: 1},
			ledger:         ledgerForMatrix(model.EpisodeStatusDownloaded, "", ""),
			item:           RSSResource{OriginalEpisode: 1, Resource: model.EpisodeResource{URL: "https://x/e01"}},
			wantAction:     DecisionCandidate,
			wantReason:     "different_resource",
			wantCandidates: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newServiceFixture(t)
			tt.sub.Name = tt.name
			require.NoError(t, fx.db.Create(&tt.sub).Error)
			if tt.ledger != nil {
				tt.ledger.SubscriptionID = tt.sub.ID
				require.NoError(t, fx.db.Create(tt.ledger).Error)
			}

			decision, err := fx.service.EvaluateRSSItem(context.Background(), &tt.sub, tt.item, tt.baseline)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAction, decision.Action)
			assert.Equal(t, tt.wantReason, decision.Reason)

			var candidateCount int64
			require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidateCount).Error)
			assert.EqualValues(t, tt.wantCandidates, candidateCount)
			if tt.wantAction == DecisionBaseline {
				ledger, err := fx.repo.GetBySubscriptionAndEpisode(tt.sub.ID, 1)
				require.NoError(t, err)
				assert.Equal(t, model.EpisodeStatusMissing, ledger.Status)
			}
		})
	}
}

func TestEvaluateRSSItemOwnedStateResourceMatchingMatrix(t *testing.T) {
	statuses := []string{
		model.EpisodeStatusDownloading,
		model.EpisodeStatusDownloaded,
		model.EpisodeStatusMarkedDownloaded,
	}
	cases := []struct {
		name           string
		current        model.EpisodeResource
		candidate      model.EpisodeResource
		wantAction     string
		wantReason     string
		wantCandidates int64
	}{
		{
			name:       "hash matches case insensitively",
			current:    model.EpisodeResource{Hash: " ABC ", URL: "https://x/old"},
			candidate:  model.EpisodeResource{Hash: "abc", URL: "https://x/new"},
			wantAction: DecisionSkip,
			wantReason: "resource_already_known",
		},
		{
			name:           "different hashes take priority over same URL",
			current:        model.EpisodeResource{Hash: "old", URL: "https://x/same"},
			candidate:      model.EpisodeResource{Hash: "new", URL: "https://x/same"},
			wantAction:     DecisionCandidate,
			wantReason:     "different_resource",
			wantCandidates: 1,
		},
		{
			name:       "candidate-only hash falls back to same URL",
			current:    model.EpisodeResource{URL: " https://x/same "},
			candidate:  model.EpisodeResource{Hash: "new", URL: "https://x/same"},
			wantAction: DecisionSkip,
			wantReason: "resource_already_known",
		},
		{
			name:       "current-only hash falls back to same URL",
			current:    model.EpisodeResource{Hash: "old", URL: "https://x/same"},
			candidate:  model.EpisodeResource{URL: " https://x/same "},
			wantAction: DecisionSkip,
			wantReason: "resource_already_known",
		},
		{
			name:       "URLs match after trimming",
			current:    model.EpisodeResource{URL: " https://x/same "},
			candidate:  model.EpisodeResource{URL: "https://x/same"},
			wantAction: DecisionSkip,
			wantReason: "resource_already_known",
		},
		{
			name:           "different URLs create candidate",
			current:        model.EpisodeResource{URL: "https://x/old"},
			candidate:      model.EpisodeResource{URL: "https://x/new"},
			wantAction:     DecisionCandidate,
			wantReason:     "different_resource",
			wantCandidates: 1,
		},
	}

	for _, status := range statuses {
		for _, tt := range cases {
			t.Run(status+"/"+tt.name, func(t *testing.T) {
				fx := newServiceFixture(t)
				sub := model.Subscription{Name: t.Name()}
				require.NoError(t, fx.db.Create(&sub).Error)
				ledger := model.SubscriptionEpisode{
					SubscriptionID:    sub.ID,
					Episode:           1,
					Status:            status,
					StatusSource:      "test",
					ActiveTorrentHash: tt.current.Hash,
					ActiveTorrentURL:  tt.current.URL,
					ActiveTitle:       tt.current.Title,
				}
				require.NoError(t, fx.db.Create(&ledger).Error)

				decision, err := fx.service.EvaluateRSSItem(context.Background(), &sub, RSSResource{
					OriginalEpisode: 1,
					Resource:        tt.candidate,
				}, false)
				require.NoError(t, err)
				assert.Equal(t, tt.wantAction, decision.Action)
				assert.Equal(t, tt.wantReason, decision.Reason)

				var candidateCount int64
				require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidateCount).Error)
				assert.Equal(t, tt.wantCandidates, candidateCount)
			})
		}
	}
}

func TestObserveRSSItemAndEnsureRangeAreIdempotent(t *testing.T) {
	fx := newServiceFixture(t)
	sub := model.Subscription{ID: 1, Name: "show", EpisodeOffset: 170, TotalEpisodes: 4}
	require.NoError(t, fx.db.Create(&sub).Error)

	observed, err := fx.service.ObserveRSSItem(&sub, 172)
	require.NoError(t, err)
	require.NotNil(t, observed)
	assert.Equal(t, 2, observed.Episode)
	assert.Equal(t, model.EpisodeStatusMissing, observed.Status)
	require.NoError(t, fx.repo.SetStatus(sub.ID, []int{2}, model.EpisodeStatusIgnored, model.EpisodeStatusSourceUser))

	again, err := fx.service.ObserveRSSItem(&sub, 172)
	require.NoError(t, err)
	assert.Equal(t, observed.ID, again.ID)
	assert.Equal(t, model.EpisodeStatusIgnored, again.Status)

	require.NoError(t, fx.service.EnsureRange(sub.ID, sub.TotalEpisodes))
	sub.TotalEpisodes = 2
	require.NoError(t, fx.service.EnsureRange(sub.ID, sub.TotalEpisodes))
	episodes, err := fx.repo.ListBySubscription(sub.ID)
	require.NoError(t, err)
	require.Len(t, episodes, 4)
	assert.Equal(t, model.EpisodeStatusIgnored, episodes[1].Status)

	invalid, err := fx.service.ObserveRSSItem(&sub, 170)
	require.NoError(t, err)
	assert.Nil(t, invalid)
}

func TestPreviewRSSItemDoesNotMutateLedgerOrCandidates(t *testing.T) {
	fx := newServiceFixture(t)
	sub := model.Subscription{ID: 1, Name: "preview"}
	require.NoError(t, fx.db.Create(&sub).Error)

	decision, err := fx.service.PreviewRSSItem(&sub, RSSResource{
		OriginalEpisode: 1,
		Resource:        model.EpisodeResource{Hash: "abc"},
	})
	require.NoError(t, err)
	assert.Equal(t, DecisionDownload, decision.Action)

	var episodeCount, candidateCount int64
	require.NoError(t, fx.db.Model(&model.SubscriptionEpisode{}).Count(&episodeCount).Error)
	require.NoError(t, fx.db.Model(&model.EpisodeResourceCandidate{}).Count(&candidateCount).Error)
	assert.Zero(t, episodeCount)
	assert.Zero(t, candidateCount)
}

func TestDownloadLifecyclePassthroughsPreserveLedgerState(t *testing.T) {
	fx := newServiceFixture(t)
	sub := model.Subscription{ID: 1, Name: "offset", EpisodeOffset: 170}
	require.NoError(t, fx.db.Create(&sub).Error)
	resource := model.EpisodeResource{Hash: "abc", URL: "https://x/1", Title: "one"}
	episode, claimed, err := fx.repo.ClaimForDownload(sub.ID, 1, resource)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, fx.service.ReleaseDownloadClaim(episode.ID, resource))
	episode, claimed, err = fx.repo.ClaimForDownload(1, 1, resource)
	require.NoError(t, err)
	require.True(t, claimed)

	err = fx.db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, fx.service.AttachDownloadInTx(tx, episode.ID, 41))
		return assert.AnError
	})
	require.ErrorIs(t, err, assert.AnError)
	require.NoError(t, fx.service.AttachDownload(episode.ID, 42))

	completedResource := model.EpisodeResource{Hash: "def", URL: "https://x/final", Title: "final"}
	completedAt := time.Now().UTC().Truncate(time.Second)
	download := model.Download{
		ID: 42, SubscriptionID: sub.ID, Episode: 171, TorrentHash: completedResource.Hash,
		TorrentURL: completedResource.URL, Title: completedResource.Title,
	}
	require.NoError(t, fx.db.Transaction(func(tx *gorm.DB) error {
		return fx.service.MarkDownloadCompletedInTx(tx, &download, &sub, completedAt)
	}))

	downloaded, err := fx.repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, downloaded.Status)
	assert.Equal(t, "def", downloaded.ActiveTorrentHash)

	require.NoError(t, fx.service.DetachDownload(42))
	detached, err := fx.repo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, detached.Status)
	assert.Nil(t, detached.ActiveDownloadID)

	episode2, claimed, err := fx.repo.ClaimForDownload(sub.ID, 2, resource)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, fx.service.AttachDownload(episode2.ID, 43))
	download2 := model.Download{
		ID: 43, SubscriptionID: sub.ID, Episode: 172, TorrentHash: "ghi",
		TorrentURL: "https://x/2", Title: "two",
	}
	require.NoError(t, fx.service.MarkDownloadCompleted(&download2, &sub, completedAt))

	episode3, claimed, err := fx.repo.ClaimForDownload(sub.ID, 3, resource)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, fx.service.AttachDownload(episode3.ID, 44))
	require.NoError(t, fx.service.MarkDownloadFailed(44))
	failed, err := fx.repo.GetBySubscriptionAndEpisode(sub.ID, 3)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, failed.Status)
	assert.Nil(t, failed.ActiveDownloadID)
}

func TestRequeueDownloadIsIdempotentForSameActiveDownload(t *testing.T) {
	fx := newServiceFixture(t)
	sub := model.Subscription{Name: "requeue same", Season: 1}
	require.NoError(t, fx.db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID, Title: "same", Episode: 1,
		TorrentURL: "magnet:same", TorrentHash: "same-hash", Status: model.DownloadStatusFailed,
		RetryCount: 1, MaxRetries: 3, LastError: "timeout",
	}
	require.NoError(t, fx.db.Create(&download).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
		ActiveTorrentHash: download.TorrentHash,
	}
	require.NoError(t, fx.db.Create(&ledger).Error)

	require.NoError(t, fx.service.RequeueDownload(&download, &sub))

	assert.Equal(t, model.DownloadStatusRetryCleanup, download.Status)
	assert.Equal(t, "same-hash", download.TorrentHash)
	after, err := fx.repo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloading, after.Status)
	require.NotNil(t, after.ActiveDownloadID)
	assert.Equal(t, download.ID, *after.ActiveDownloadID)
}

func TestRequeueDownloadRejectsOwnedTerminalEpisodeStates(t *testing.T) {
	for _, status := range []string{
		model.EpisodeStatusDownloaded,
		model.EpisodeStatusMarkedDownloaded,
		model.EpisodeStatusIgnored,
	} {
		t.Run(status, func(t *testing.T) {
			fx := newServiceFixture(t)
			sub := model.Subscription{Name: status, Season: 1}
			require.NoError(t, fx.db.Create(&sub).Error)
			download := model.Download{
				SubscriptionID: sub.ID, Title: status, Episode: 1,
				TorrentURL: "magnet:" + status, TorrentHash: "hash-" + status,
				Status: model.DownloadStatusFailed, RetryCount: 3, MaxRetries: 3,
			}
			require.NoError(t, fx.db.Create(&download).Error)
			ledger := model.SubscriptionEpisode{
				SubscriptionID: sub.ID, Episode: 1, Status: status,
				StatusSource: model.EpisodeStatusSourceUser,
			}
			require.NoError(t, fx.db.Create(&ledger).Error)

			err := fx.service.RequeueDownload(&download, &sub)
			require.Error(t, err)
			assert.Equal(t, model.DownloadStatusFailed, download.Status)
			assert.Equal(t, "hash-"+status, download.TorrentHash)
		})
	}
}

func TestRequeueDownloadCollectionOnlyResetsDownload(t *testing.T) {
	fx := newServiceFixture(t)
	download := model.Download{
		Title: "collection", Episode: 0, TorrentURL: "magnet:collection", TorrentHash: "collection-hash",
		Status: model.DownloadStatusFailed, RetryCount: 2, MaxRetries: 3, LastError: "timeout",
	}
	require.NoError(t, fx.db.Create(&download).Error)

	require.NoError(t, fx.service.RequeueDownload(&download, nil))

	assert.Equal(t, model.DownloadStatusRetryCleanup, download.Status)
	assert.Equal(t, "collection-hash", download.TorrentHash)
	var ledgerCount int64
	require.NoError(t, fx.db.Model(&model.SubscriptionEpisode{}).Count(&ledgerCount).Error)
	assert.Zero(t, ledgerCount)
}

func TestRequeueDownloadSaveFailureRollsBackEpisodeClaim(t *testing.T) {
	fx := newServiceFixture(t)
	sub := model.Subscription{Name: "requeue rollback", Season: 1}
	require.NoError(t, fx.db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID, Title: "rollback", Episode: 1,
		TorrentURL: "magnet:rollback", TorrentHash: "rollback-hash",
		Status: model.DownloadStatusFailed, RetryCount: 3, MaxRetries: 3,
	}
	require.NoError(t, fx.db.Create(&download).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusMissing,
		StatusSource: model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, fx.db.Create(&ledger).Error)
	require.NoError(t, fx.db.Exec(`
		CREATE TRIGGER fail_requeue_download
		BEFORE UPDATE OF status ON downloads
		WHEN NEW.status = 'retry_cleanup'
		BEGIN
			SELECT RAISE(ABORT, 'injected requeue save failure');
		END;
	`).Error)

	err := fx.service.RequeueDownload(&download, &sub)
	require.Error(t, err)
	assert.Equal(t, model.DownloadStatusFailed, download.Status)
	assert.Equal(t, "rollback-hash", download.TorrentHash)
	after, reloadErr := fx.repo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.EpisodeStatusMissing, after.Status)
	assert.Nil(t, after.ActiveDownloadID)
	var persisted model.Download
	require.NoError(t, fx.db.First(&persisted, download.ID).Error)
	assert.Equal(t, model.DownloadStatusFailed, persisted.Status)
}

func TestRequeueDownloadKeepsDistinctCleanupCheckpoints(t *testing.T) {
	fx := newServiceFixture(t)
	sub := model.Subscription{Name: "parallel retry", Status: "active"}
	require.NoError(t, fx.db.Create(&sub).Error)
	downloads := []model.Download{
		{SubscriptionID: sub.ID, Title: "one", TorrentURL: "magnet:one", TorrentHash: "old-one", Status: model.DownloadStatusFailed},
		{SubscriptionID: sub.ID, Title: "two", TorrentURL: "magnet:two", TorrentHash: "old-two", Status: model.DownloadStatusFailed},
	}
	require.NoError(t, fx.db.Create(&downloads).Error)

	require.NoError(t, fx.service.RequeueDownload(&downloads[0], nil))
	require.NoError(t, fx.service.RequeueDownload(&downloads[1], nil))

	assert.Equal(t, model.DownloadStatusRetryCleanup, downloads[0].Status)
	assert.Equal(t, model.DownloadStatusRetryCleanup, downloads[1].Status)
	assert.Equal(t, "old-one", downloads[0].TorrentHash)
	assert.Equal(t, "old-two", downloads[1].TorrentHash)
}

func TestPersistDownloadFailureRollsBackWhenLedgerReleaseFails(t *testing.T) {
	fx := newServiceFixture(t)
	sub := model.Subscription{Name: "atomic failure", Status: "active"}
	require.NoError(t, fx.db.Create(&sub).Error)
	download := model.Download{
		SubscriptionID: sub.ID, Title: "episode one", Episode: 1,
		TorrentURL: "magnet:atomic", TorrentHash: "atomic-hash", Status: model.DownloadStatusDownloading,
		RetryCount: 1, MaxRetries: 1,
	}
	require.NoError(t, fx.db.Create(&download).Error)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
		ActiveTorrentHash: download.TorrentHash,
	}
	require.NoError(t, fx.db.Create(&ledger).Error)
	require.NoError(t, fx.db.Exec(`CREATE TRIGGER fail_failure_release BEFORE UPDATE OF status ON subscription_episodes WHEN NEW.status = 'missing' BEGIN SELECT RAISE(ABORT, 'injected release failure'); END;`).Error)

	download.Status = model.DownloadStatusFailed
	download.LastError = "terminal failure"
	download.ErrorMessage = "terminal failure"
	err := fx.service.PersistDownloadFailure(&download, true)
	require.Error(t, err)
	persisted := model.Download{}
	require.NoError(t, fx.db.First(&persisted, download.ID).Error)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
	after, reloadErr := fx.repo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.EpisodeStatusDownloading, after.Status)
	require.NotNil(t, after.ActiveDownloadID)

	require.NoError(t, fx.db.Exec("DROP TRIGGER fail_failure_release").Error)
	require.NoError(t, fx.service.PersistDownloadFailure(&download, true))
	require.NoError(t, fx.db.First(&persisted, download.ID).Error)
	assert.Equal(t, model.DownloadStatusFailed, persisted.Status)
	after, reloadErr = fx.repo.GetBySubscriptionAndEpisode(sub.ID, 1)
	require.NoError(t, reloadErr)
	assert.Equal(t, model.EpisodeStatusMissing, after.Status)
	assert.Nil(t, after.ActiveDownloadID)
}

func ledgerForMatrix(status, hash, url string) *model.SubscriptionEpisode {
	return &model.SubscriptionEpisode{
		Episode:           1,
		Status:            status,
		StatusSource:      "test",
		ActiveTorrentHash: hash,
		ActiveTorrentURL:  url,
	}
}
