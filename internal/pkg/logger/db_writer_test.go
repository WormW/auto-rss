package logger

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDBWriterReserveCleanupHonorsInterval(t *testing.T) {
	writer := &DBWriter{cleanupInterval: time.Hour}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	require.True(t, writer.reserveCleanup(now))
	require.False(t, writer.reserveCleanup(now.Add(30*time.Minute)))
	require.True(t, writer.reserveCleanup(now.Add(time.Hour)))
}

func TestDBWriterReserveCleanupAllowsOneConcurrentCaller(t *testing.T) {
	writer := &DBWriter{cleanupInterval: time.Hour}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if writer.reserveCleanup(now) {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()

	require.EqualValues(t, 1, winners.Load())
}

func TestDBWriterWriteReservesCleanupAfterSuccessfulInsert(t *testing.T) {
	db := newDBWriterTestDB(t)
	writer := NewDBWriter(db)
	writer.cleanupInterval = time.Hour
	payload := []byte(`{"level":"info","msg":"HTTP Request","caller":"internal/api/middleware/logger.go","path":"/api/v1/subscriptions"}`)

	n, err := writer.Write(payload)

	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.Eventually(t, func() bool {
		var count int64
		if err := db.Model(&model.Log{}).Count(&count).Error; err != nil {
			return false
		}
		return count == 1 && cleanupReserved(writer)
	}, time.Second, 10*time.Millisecond)
}

func TestDBWriterWriteDoesNotReserveCleanupWhenInsertFails(t *testing.T) {
	db := newDBWriterTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	writer := NewDBWriter(db)
	writer.cleanupInterval = time.Hour
	payload := []byte(`{"level":"info","msg":"HTTP Request","caller":"internal/api/middleware/logger.go","path":"/api/v1/subscriptions"}`)

	n, err := writer.Write(payload)

	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.Never(t, func() bool {
		return cleanupReserved(writer)
	}, 100*time.Millisecond, 10*time.Millisecond)
}

func newDBWriterTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	return db
}

func cleanupReserved(writer *DBWriter) bool {
	writer.cleanupMu.Lock()
	defer writer.cleanupMu.Unlock()

	return !writer.lastCleanup.IsZero()
}
