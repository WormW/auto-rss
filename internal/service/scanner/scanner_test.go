package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestScannerRejectsDownloadRoot(t *testing.T) {
	root, svc, sub := newScannerFixture(t)

	result, err := svc.Scan(&sub, &Request{FolderPath: root, DryRun: true})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "refusing to scan download root")
}

func TestScannerRejectsPathOutsideDownloadRoot(t *testing.T) {
	_, svc, sub := newScannerFixture(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "Outside Show S01E01.mkv"), []byte("fixture"), 0644))

	result, err := svc.Scan(&sub, &Request{FolderPath: outside, DryRun: true})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "folder_path must be inside download_path")
}

func TestScannerAllowsSubscriptionFolder(t *testing.T) {
	root, svc, sub := newScannerFixture(t)
	showDir := filepath.Join(root, "Scanner Show")
	require.NoError(t, os.MkdirAll(showDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(showDir, "Scanner Show S01E01.mkv"), []byte("fixture"), 0644))

	result, err := svc.Scan(&sub, &Request{FolderPath: showDir, DryRun: true})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Scanned)
	assert.Equal(t, 1, result.Matched)
}

func newScannerFixture(t *testing.T) (string, *Scanner, model.Subscription) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}, &model.Download{}, &model.Config{}))

	root := t.TempDir()
	configRepo := repository.NewConfigRepository(db)
	require.NoError(t, configRepo.Set("download_path", root))

	subRepo := repository.NewSubscriptionRepository(db)
	downloadRepo := repository.NewDownloadRepository(db)
	sub := model.Subscription{
		Name:          "Scanner Show",
		Season:        1,
		LatestEpisode: 1,
		Enabled:       true,
		Status:        "active",
	}
	require.NoError(t, subRepo.Create(&sub))

	return root, NewScanner(db, subRepo, downloadRepo, configRepo), sub
}
