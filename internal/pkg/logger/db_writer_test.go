package logger

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
