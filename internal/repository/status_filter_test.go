package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDownloadStatusFilterSimple(t *testing.T) {
	// 每个测试函数使用独立内存数据库
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	db.AutoMigrate(&model.Subscription{}, &model.Download{})

	repo := NewDownloadRepository(db)

	// 插入测试数据
	sub := model.Subscription{Name: "s1", RssURL: "https://x.com"}
	db.Create(&sub)
	db.Create(&model.Download{SubscriptionID: sub.ID, Title: "d1", Status: "downloading", TorrentURL: "m:1", TorrentHash: "h1"})
	db.Create(&model.Download{SubscriptionID: sub.ID, Title: "s1", Status: "stalled", TorrentURL: "m:2", TorrentHash: "h2"})
	db.Create(&model.Download{SubscriptionID: sub.ID, Title: "c1", Status: "completed", TorrentURL: "m:3", TorrentHash: "h3"})

	// 测试 downloading 过滤
	dl, _, _ := repo.List(0, 10, "downloading")
	if len(dl) != 1 || dl[0].Status != "downloading" {
		t.Fatalf("expected 1 downloading, got %d", len(dl))
	}

	// 测试 stalled 过滤
	st, _, _ := repo.List(0, 10, "stalled")
	if len(st) != 1 || st[0].Status != "stalled" {
		t.Fatalf("expected 1 stalled, got %d", len(st))
	}
}