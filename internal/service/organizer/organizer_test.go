package organizer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type failingOrganizerMover struct{ FileMover }

func (f *failingOrganizerMover) Move(string, string) error { return errors.New("move failed") }

type organizerEpisodeCompletionMock struct {
	completedInTx bool
}

func (*organizerEpisodeCompletionMock) MarkDownloadCompleted(*model.Download, *model.Subscription, time.Time) error {
	return nil
}

func (m *organizerEpisodeCompletionMock) MarkDownloadCompletedInTx(*gorm.DB, *model.Download, *model.Subscription, time.Time) error {
	m.completedInTx = true
	return nil
}

func (*organizerEpisodeCompletionMock) MarkDownloadFailed(uint) error { return nil }
func (*organizerEpisodeCompletionMock) DetachDownload(uint) error     { return nil }

func TestFileOrganizerCompletesEpisodeInDownloadTransaction(t *testing.T) {
	watchDir := t.TempDir()
	destDir := t.TempDir()
	filePath := filepath.Join(watchDir, "[Group] Ledger Show - 01 [1080p].mkv")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0600))
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/organizer-ledger.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))
	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	sub := &model.Subscription{Name: "Ledger Show", Season: 1, RssURL: "https://example.test/rss"}
	require.NoError(t, subRepo.Create(sub))
	download := &model.Download{
		SubscriptionID: sub.ID,
		Title:          "Ledger Show - 01",
		Episode:        1,
		TorrentURL:     "https://example.test/1.torrent",
		TorrentHash:    "organizer-ledger",
		Status:         model.DownloadStatusDownloading,
	}
	require.NoError(t, downloadRepo.Create(download))
	episodes := &organizerEpisodeCompletionMock{}
	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", episodes)
	require.NoError(t, err)
	defer organizer.Stop()

	require.NoError(t, organizer.organizeFile(filePath))

	assert.True(t, episodes.completedInTx)
	persisted, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusCompleted, persisted.Status)
}

func TestFileOrganizerMoveFailureKeepsDownloadActive(t *testing.T) {
	watchDir := t.TempDir()
	destDir := t.TempDir()
	filePath := filepath.Join(watchDir, "[Group] Move Failure Show - 01 [1080p].mkv")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0600))
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/organizer-move-failure.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))
	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	sub := &model.Subscription{Name: "Move Failure Show", Season: 1, RssURL: "https://example.test/rss"}
	require.NoError(t, subRepo.Create(sub))
	download := &model.Download{
		SubscriptionID: sub.ID, Title: "Move Failure Show - 01", Episode: 1,
		TorrentURL: "https://example.test/1.torrent", TorrentHash: "organizer-move-failure",
		Status: model.DownloadStatusDownloading,
	}
	require.NoError(t, downloadRepo.Create(download))
	episodes := &organizerEpisodeCompletionMock{}
	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", episodes)
	require.NoError(t, err)
	defer organizer.Stop()
	organizer.mover = &failingOrganizerMover{FileMover: NewFileMover()}

	err = organizer.organizeFile(filePath)
	require.Error(t, err)
	persisted, err := downloadRepo.GetByID(download.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DownloadStatusDownloading, persisted.Status)
	assert.False(t, episodes.completedInTx)
}

func TestFileOrganizer_organizeFile(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		setupSub      func(repo repository.SubscriptionRepository) (*model.Subscription, error)
		setupDownload func(repo repository.DownloadRepository, subID uint) (*model.Download, error)
		wantMoved     bool
		wantErr       bool
	}{
		{
			name:     "success - moves file and updates download",
			filename: "[Group] Anime Title - 01 [1080p].mkv",
			setupSub: func(repo repository.SubscriptionRepository) (*model.Subscription, error) {
				sub := &model.Subscription{
					Name:   "Anime Title",
					Season: 1,
					RssURL: "http://test.com/rss",
				}
				err := repo.Create(sub)
				return sub, err
			},
			setupDownload: func(repo repository.DownloadRepository, subID uint) (*model.Download, error) {
				dl := &model.Download{
					SubscriptionID: subID,
					Title:          "Anime Title - 01",
					Episode:        1,
					Status:         "completed",
					TorrentURL:     "http://test.com/torrent",
				}
				err := repo.Create(dl)
				return dl, err
			},
			wantMoved: true,
			wantErr:   false,
		},
		{
			name:     "no matching subscription - file stays",
			filename: "[Group] Unknown Anime - 01 [1080p].mkv",
			setupSub: func(repo repository.SubscriptionRepository) (*model.Subscription, error) {
				// Don't create any subscription
				return nil, nil
			},
			setupDownload: func(repo repository.DownloadRepository, subID uint) (*model.Download, error) {
				// Don't create download
				return nil, nil
			},
			wantMoved: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directories
			watchDir := t.TempDir()
			destDir := t.TempDir()

			// Create test file
			testFile := filepath.Join(watchDir, tt.filename)
			err := os.WriteFile(testFile, []byte("test content"), 0644)
			require.NoError(t, err)

			// Setup in-memory database
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

			// Create repositories
			subRepo := repository.NewSubscriptionRepository(db)
			downloadRepo := repository.NewDownloadRepository(db)

			// Setup subscription and download
			var sub *model.Subscription
			if tt.setupSub != nil {
				sub, _ = tt.setupSub(subRepo)
			}
			if tt.setupDownload != nil && sub != nil {
				_, _ = tt.setupDownload(downloadRepo, sub.ID)
			}

			// Create organizer
			organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
			require.NoError(t, err)

			// Test organize
			err = organizer.organizeFile(testFile)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify file was moved or not
			if tt.wantMoved {
				// File should not exist in watch dir
				_, err = os.Stat(testFile)
				assert.True(t, os.IsNotExist(err), "file should be moved from watch dir")

				// File should exist in dest dir
				destFiles, _ := os.ReadDir(destDir)
				assert.Greater(t, len(destFiles), 0, "file should exist in dest dir")
			} else {
				// File should still exist in watch dir
				_, err = os.Stat(testFile)
				assert.NoError(t, err, "file should remain in watch dir")
			}
		})
	}
}

func TestFileOrganizer_organizeFile_NonExistent(t *testing.T) {
	watchDir := t.TempDir()
	destDir := t.TempDir()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)

	// Try to organize non-existent file
	nonExistentFile := filepath.Join(watchDir, "non-existent.mkv")
	err = organizer.organizeFile(nonExistentFile)

	assert.Error(t, err)
}

func TestFileOrganizer_Start(t *testing.T) {
	watchDir := t.TempDir()
	destDir := t.TempDir()

	// Create a test file in watch dir
	testFile := filepath.Join(watchDir, "test.mkv")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)

	// Start should not error
	err = organizer.Start()
	assert.NoError(t, err)

	// Stop the organizer
	organizer.Stop()
}

func TestFileOrganizer_organizeFile_UpdatesDownloadRecord(t *testing.T) {
	// Create temp directories
	watchDir := t.TempDir()
	destDir := t.TempDir()

	// Create test file
	filename := "[Group] Test Anime - 01 [1080p].mkv"
	testFile := filepath.Join(watchDir, filename)
	err := os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Setup in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

	// Create repositories
	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	// Create subscription
	sub := &model.Subscription{
		Name:   "Test Anime",
		Season: 1,
		RssURL: "http://test.com/rss",
	}
	err = subRepo.Create(sub)
	require.NoError(t, err)

	// Create download record
	dl := &model.Download{
		SubscriptionID: sub.ID,
		Title:          "Test Anime - 01",
		Episode:        1,
		Status:         model.DownloadStatusCompleted,
		TorrentURL:     "http://test.com/torrent",
		TorrentHash:    "abc123def456",
	}
	err = downloadRepo.Create(dl)
	require.NoError(t, err)

	// Create organizer
	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)

	// Organize file
	err = organizer.organizeFile(testFile)
	require.NoError(t, err)

	// Verify download record was updated with new path
	updatedDL, err := downloadRepo.GetByID(dl.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, updatedDL.FilePath, "FilePath should be updated")
	assert.Contains(t, updatedDL.FilePath, destDir, "FilePath should be in destination directory")
	assert.Equal(t, model.DownloadStatusCompleted, updatedDL.Status, "Status should remain completed")
}

func TestFileOrganizer_organizeFile_NoMatchingSubscription(t *testing.T) {
	// Create temp directories
	watchDir := t.TempDir()
	destDir := t.TempDir()

	// Create test file with name that won't match any subscription
	filename := "[Group] Unknown Show XYZ - 01 [1080p].mkv"
	testFile := filepath.Join(watchDir, filename)
	err := os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Setup in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

	// Create repositories
	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	// Create a different subscription (not matching our file)
	sub := &model.Subscription{
		Name:   "Different Anime",
		Season: 1,
		RssURL: "http://test.com/rss",
	}
	err = subRepo.Create(sub)
	require.NoError(t, err)

	// Create organizer
	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)

	// Try to organize file - should fail because no matching subscription
	err = organizer.organizeFile(testFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no matching subscription")

	// Verify file was NOT moved
	_, err = os.Stat(testFile)
	assert.NoError(t, err, "File should remain in watch directory")
}

func TestFileOrganizer_organizeFile_PathTraversalPrevention(t *testing.T) {
	// Create temp directories
	watchDir := t.TempDir()
	destDir := t.TempDir()

	// Setup in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

	// Create repositories
	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	// Create organizer
	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)

	// Try to organize file outside watch directory (path traversal attempt)
	outsideFile := "/etc/passwd"
	err = organizer.organizeFile(outsideFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escapes watch directory")
}

func TestFileOrganizer_TriggerScan(t *testing.T) {
	watchDir := t.TempDir()
	destDir := t.TempDir()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)

	// Start organizer first
	err = organizer.Start()
	require.NoError(t, err)
	defer organizer.Stop()

	// Trigger scan should not panic
	organizer.TriggerScan()
}

func TestFileOrganizer_PreventsDuplicateScans(t *testing.T) {
	watchDir := t.TempDir()
	destDir := t.TempDir()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)

	require.True(t, organizer.beginScan())
	require.False(t, organizer.beginScan())

	organizer.endScan()
	require.True(t, organizer.beginScan())
	organizer.endScan()
}

func TestFileOrganizer_StartDoesNotAutoScanExistingFiles(t *testing.T) {
	watchDir := t.TempDir()
	destDir := t.TempDir()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	sub := &model.Subscription{
		Name:   "Quiet Show",
		Season: 1,
		RssURL: "http://test.com/rss",
	}
	require.NoError(t, subRepo.Create(sub))

	existingFile := filepath.Join(watchDir, "[Group] Quiet Show - 01 [1080p].mkv")
	require.NoError(t, os.WriteFile(existingFile, []byte("test"), 0644))

	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)
	require.NoError(t, organizer.Start())
	defer organizer.Stop()

	time.Sleep(3 * time.Second)

	_, err = os.Stat(existingFile)
	require.NoError(t, err, "startup should not auto-process existing files")
}

func TestFileOrganizer_StartAutoScansExistingFilesWhenEnabled(t *testing.T) {
	watchDir := t.TempDir()
	destDir := t.TempDir()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}))

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	sub := &model.Subscription{
		Name:   "Auto Scan Show",
		Season: 1,
		RssURL: "http://test.com/rss",
	}
	require.NoError(t, subRepo.Create(sub))

	existingFile := filepath.Join(watchDir, "[Group] Auto Scan Show - 01 [1080p].mkv")
	require.NoError(t, os.WriteFile(existingFile, []byte("test"), 0644))

	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)
	organizer.SetScanOnStart(true)
	require.NoError(t, organizer.Start())
	defer organizer.Stop()

	require.Eventually(t, func() bool {
		_, err := os.Stat(existingFile)
		return os.IsNotExist(err)
	}, 9*time.Second, 200*time.Millisecond)
}

func TestFileOrganizer_Stop(t *testing.T) {
	watchDir := t.TempDir()
	destDir := t.TempDir()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)

	organizer, err := NewFileOrganizer(watchDir, destDir, subRepo, downloadRepo, db, nil, "", nil)
	require.NoError(t, err)

	// Start and then stop
	err = organizer.Start()
	require.NoError(t, err)

	// Stop should not panic or error
	organizer.Stop()
}
