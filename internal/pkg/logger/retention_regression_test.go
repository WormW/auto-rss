package logger

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/require"
)

func TestDBWriterCleanupCatchesUpWithLogBacklog(t *testing.T) {
	db := newDBWriterTestDB(t)
	now := time.Now()
	logs := make([]model.Log, maxLogCount+3*cleanupBatch)
	for i := range logs {
		logs[i] = model.Log{Level: "info", Message: "download status", CreatedAt: now.Add(-time.Minute)}
	}
	require.NoError(t, db.CreateInBatches(&logs, 100).Error)

	writer := NewDBWriter(db)
	_, err := writer.Write([]byte(`{"level":"warn","msg":"newest diagnostic"}`))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		var count int64
		return db.Model(&model.Log{}).Count(&count).Error == nil && count == maxLogCount
	}, time.Second, 10*time.Millisecond, "one cleanup cycle must restore the configured retention limit")

	var newest model.Log
	require.NoError(t, db.Order("id DESC").First(&newest).Error)
	require.Equal(t, "newest diagnostic", newest.Message)
}
