package downloader

import (
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
)

type blockingCompletionHandler struct {
	started chan struct{}
	release chan struct{}
}

func (h *blockingCompletionHandler) HandleComplete(_ *model.Download, _ *TorrentInfo, _ *model.Subscription) error {
	close(h.started)
	<-h.release
	return nil
}

func TestHandleCompletionAsyncDoesNotBlockMonitor(t *testing.T) {
	handler := &blockingCompletionHandler{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	monitor := &DownloadMonitor{completionHandler: handler}

	returned := make(chan struct{})
	go func() {
		monitor.handleCompletionAsync(&model.Download{}, &TorrentInfo{}, &model.Subscription{})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("completion dispatch blocked the download monitor")
	}

	select {
	case <-handler.started:
	case <-time.After(time.Second):
		t.Fatal("completion handler was not started")
	}

	close(handler.release)
}
