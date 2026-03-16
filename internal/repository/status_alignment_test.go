package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestDownloadStatusAlignmentVerifies download status filter returns correct results
func TestDownloadStatusAlignmentVerifies(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	db.AutoMigrate(&model.Subscription{}, &model.Download{})

	repo := NewDownloadRepository(db)

	// 插入测试数据
	sub := model.Subscription{Name: "sub1", RssURL: "https://x.com"}
	db.Create(&sub)
	db.Create(&model.Download{SubscriptionID: sub.ID, Title: "dl1", Status: "downloading", TorrentURL: "m:1", TorrentHash: "h1"})
	db.Create(&model.Download{SubscriptionID: sub.ID, Title: "st1", Status: "stalled", TorrentURL: "m:2", TorrentHash: "h2"})

	// 验证 downloading 过滤
	dl, _, _ := repo.List(0, 10, "downloading")
	if len(dl) != 1 || dl[0].Status != "downloading" {
		t.Fatalf("downloading filter failed: got %d", len(dl))
	}

	// 验证 stalled 过滤
	st, _, _ := repo.List(0, 10, "stalled")
	if len(st) != 1 || st[0].Status != "stalled" {
		t.Fatalf("stalled filter failed: got %d", len(st))
	}
}