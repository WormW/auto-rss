package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDownloadStatusFilterMinimal(t *testing.T) {
	// 每个测试使用独立的内存数据库
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open DB: %v", err)
	}
	db.AutoMigrate(&model.Subscription{}, &model.Download{})

	repo := NewDownloadRepository(db)

	// 创建测试数据
	sub := model.Subscription{Name: "test", RssURL: "https://test.com"}
	db.Create(&sub)

	db.Create(&model.Download{
		SubscriptionID: sub.ID, Title: "dl1", Status: "downloading", TorrentURL: "magnet:1", TorrentHash: "h1"})
	db.Create(&model.Download{
		SubscriptionID: sub.ID, Title: "st1", Status: "stalled", TorrentURL: "magnet:2", TorrentHash: "h2"})
	db.Create(&model.Download{
		SubscriptionID: sub.ID, Title: "co1", Status: "completed", TorrentURL: "magnet:3", TorrentHash: "h3"})

	// 验证 downloading 过滤
	dl, _, _ := repo.List(0, 10, "downloading")
	if len(dl) != 1 || dl[0].Status != "downloading" {
		t.Fatalf("expected 1 downloading, got %d", len(dl))
	}

	// 验证 stalled 过滤
	st, _, _ := repo.List(0, 10, "stalled")
	if len(st) != 1 || st[0].Status != "stalled" {
		t.Fatalf("expected 1 stalled, got %d", len(st))
	}
}