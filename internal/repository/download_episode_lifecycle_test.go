package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDownloadRepositoryDeleteDetachesEpisodeLedger(t *testing.T) {
	db, episodeRepo := setupEpisodeRepository(t)
	downloadRepo := NewDownloadRepository(db)
	download := model.Download{
		SubscriptionID: 1,
		Title:          "episode 1",
		Episode:        1,
		TorrentURL:     "https://example.test/1.torrent",
		TorrentHash:    "delete-downloading",
		Status:         model.DownloadStatusDownloading,
	}
	require.NoError(t, downloadRepo.Create(&download))
	ledger := model.SubscriptionEpisode{
		SubscriptionID:    1,
		Episode:           1,
		Status:            model.EpisodeStatusDownloading,
		StatusSource:      model.EpisodeStatusSourceAutomatic,
		ActiveDownloadID:  &download.ID,
		ActiveTorrentHash: download.TorrentHash,
		ActiveTorrentURL:  download.TorrentURL,
		ActiveTitle:       download.Title,
	}
	require.NoError(t, db.Create(&ledger).Error)

	require.NoError(t, downloadRepo.Delete(download.ID))

	after, err := episodeRepo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusMissing, after.Status)
	assert.Nil(t, after.ActiveDownloadID)
	assert.Empty(t, after.ActiveTorrentHash)
	assert.Empty(t, after.ActiveTorrentURL)
	assert.Empty(t, after.ActiveTitle)
}

func TestDownloadRepositoryDeleteKeepsDownloadedEpisodeResource(t *testing.T) {
	db, episodeRepo := setupEpisodeRepository(t)
	downloadRepo := NewDownloadRepository(db)
	download := model.Download{Title: "done", TorrentURL: "https://example.test/done", TorrentHash: "done-hash", Status: model.DownloadStatusCompleted}
	require.NoError(t, downloadRepo.Create(&download))
	ledger := model.SubscriptionEpisode{
		SubscriptionID: 1, Episode: 1, Status: model.EpisodeStatusDownloaded,
		StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
		ActiveTorrentHash: download.TorrentHash, ActiveTorrentURL: download.TorrentURL, ActiveTitle: download.Title,
	}
	require.NoError(t, db.Create(&ledger).Error)

	require.NoError(t, downloadRepo.Delete(download.ID))

	after, err := episodeRepo.GetBySubscriptionAndEpisode(1, 1)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeStatusDownloaded, after.Status)
	assert.Nil(t, after.ActiveDownloadID)
	assert.Equal(t, "done-hash", after.ActiveTorrentHash)
	assert.Equal(t, download.TorrentURL, after.ActiveTorrentURL)
	assert.Equal(t, download.Title, after.ActiveTitle)
}

func TestDownloadRepositoryBulkDeletesDetachMatchingEpisodes(t *testing.T) {
	tests := []struct {
		name   string
		remove func(DownloadRepository, []model.Download) error
	}{
		{
			name: "batch",
			remove: func(repo DownloadRepository, downloads []model.Download) error {
				return repo.BatchDelete([]uint{downloads[0].ID, downloads[1].ID})
			},
		},
		{
			name: "status",
			remove: func(repo DownloadRepository, _ []model.Download) error {
				return repo.DeleteByStatus(model.DownloadStatusFailed)
			},
		},
		{
			name: "all",
			remove: func(repo DownloadRepository, _ []model.Download) error {
				return repo.DeleteAll()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, episodeRepo := setupEpisodeRepository(t)
			downloadRepo := NewDownloadRepository(db)
			downloads := []model.Download{
				{Title: "one", Episode: 1, TorrentURL: "https://example.test/1", TorrentHash: tt.name + "-1", Status: model.DownloadStatusFailed},
				{Title: "two", Episode: 2, TorrentURL: "https://example.test/2", TorrentHash: tt.name + "-2", Status: model.DownloadStatusFailed},
			}
			require.NoError(t, db.Create(&downloads).Error)
			for i := range downloads {
				ledger := model.SubscriptionEpisode{
					SubscriptionID: 1, Episode: i + 1, Status: model.EpisodeStatusDownloading,
					StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &downloads[i].ID,
					ActiveTorrentHash: downloads[i].TorrentHash,
				}
				require.NoError(t, db.Create(&ledger).Error)
			}

			require.NoError(t, tt.remove(downloadRepo, downloads))

			for episodeNumber := 1; episodeNumber <= 2; episodeNumber++ {
				after, err := episodeRepo.GetBySubscriptionAndEpisode(1, episodeNumber)
				require.NoError(t, err)
				assert.Equal(t, model.EpisodeStatusMissing, after.Status)
				assert.Nil(t, after.ActiveDownloadID)
				assert.Empty(t, after.ActiveTorrentHash)
			}
		})
	}
}

func TestDownloadRepositoryDeleteSupportsLegacySchemaWithoutEpisodeTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/legacy.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Download{}))
	repo := NewDownloadRepository(db)
	download := model.Download{Title: "legacy", TorrentURL: "https://example.test/legacy", Status: model.DownloadStatusFailed}
	require.NoError(t, repo.Create(&download))

	require.NoError(t, repo.Delete(download.ID))
}
