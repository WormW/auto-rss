package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDownloadStatusFilter(t *testing.T) {
	// 初始化测试数据库
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Download{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	repo := NewDownloadRepository(db)

	// 插入测试数据
	testSub := model.Subscription{Name: "test", RssURL: "https://example.com/rss"}
	if err := db.Create(&testSub).Error; err != nil {
		t.Fatalf("failed to create test subscription: %v", err)
	}

	testData := []model.Download{
		{SubscriptionID: testSub.ID, Title: "active", Status: "downloading", TorrentURL: "magnet:1", TorrentHash: "hash1"},
		{SubscriptionID: testSub.ID, Title: "stuck", Status: "stalled", TorrentURL: "magnet:2", TorrentHash: "hash2"},
		{SubscriptionID: testSub.ID, Title: "done", Status: "completed", TorrentURL: "magnet:3", TorrentHash: "hash3"},
	}
	for i := range testData {
		if err := repo.Create(&testData[i]); err != nil {
			t.Fatalf("failed to create test data: %v", err)
		}
	}

	// 测试下载中筛选
	dlRes, dlTotal, err := repo.List(0, 10, "downloading")
	if err != nil {
		t.Fatalf("failed to filter downloading: %v", err)
	}
	if dlTotal != 1 || dlRes[0].Status != "downloading" {
		t.Fatalf("downloading filter returned %d items, want 1 downloading", dlTotal)
	}

	// 测试停滞筛选
	stRes, stTotal, err := repo.List(0, 10, "stalled")
	if err != nil {
		t.Fatalf("failed to filter stalled: %v", err)
	}
	if stTotal != 1 || stRes[0].Status != "stalled" {
		t.Fatalf("stalled filter returned %d items, want 1 stalled", stTotal)
	}
}
