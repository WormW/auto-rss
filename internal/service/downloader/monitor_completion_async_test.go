package downloader

import (
	"sync"
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

type serialCompletionHandler struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	mu       sync.Mutex
	active   int
	max      int
}

func (h *serialCompletionHandler) HandleComplete(_ *model.Download, _ *TorrentInfo, _ *model.Subscription) error {
	h.mu.Lock()
	h.active++
	if h.active > h.max {
		h.max = h.active
	}
	h.mu.Unlock()
	h.started <- struct{}{}
	<-h.release
	h.mu.Lock()
	h.active--
	h.mu.Unlock()
	h.finished <- struct{}{}
	return nil
}

func TestHandleCompletionAsyncBoundsFilesystemWork(t *testing.T) {
	handler := &serialCompletionHandler{
		started:  make(chan struct{}, 2),
		release:  make(chan struct{}, 2),
		finished: make(chan struct{}, 2),
	}
	monitor := &DownloadMonitor{
		completionHandler: handler,
		completionSlots:   make(chan struct{}, 1),
	}

	monitor.handleCompletionAsync(&model.Download{ID: 1}, &TorrentInfo{}, &model.Subscription{})
	select {
	case <-handler.started:
	case <-time.After(time.Second):
		t.Fatal("first completion handler did not start")
	}

	monitor.handleCompletionAsync(&model.Download{ID: 2}, &TorrentInfo{}, &model.Subscription{})
	select {
	case <-handler.started:
		t.Fatal("second completion handler started before the first released its slot")
	case <-time.After(100 * time.Millisecond):
	}

	handler.release <- struct{}{}
	select {
	case <-handler.finished:
	case <-time.After(time.Second):
		t.Fatal("first completion handler did not finish")
	}
	select {
	case <-handler.started:
	case <-time.After(time.Second):
		t.Fatal("second completion handler did not start after a slot was released")
	}
	handler.release <- struct{}{}
	select {
	case <-handler.finished:
	case <-time.After(time.Second):
		t.Fatal("second completion handler did not finish")
	}

	handler.mu.Lock()
	maxActive := handler.max
	handler.mu.Unlock()
	if maxActive != 1 {
		t.Fatalf("maximum concurrent completion handlers = %d, want 1", maxActive)
	}
}
