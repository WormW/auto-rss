package subscription

import (
	"strings"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/pkg/utils"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/downloader"
)

// CollectionDownloader 合集种子下载服务
type CollectionDownloader interface {
	// Download 下载合集种子并创建下载记录
	// 返回创建的下载记录（如果成功）
	Download(subscription *model.Subscription) (*model.Download, error)

	// DownloadAsync 异步下载合集（内部调用 Download）
	DownloadAsync(subscription *model.Subscription)
}

type collectionDownloader struct {
	qbClient     downloader.QBittorrentClient
	downloadRepo collectionDownloadRepository
	configRepo   repository.ConfigRepository
	basePath     string
}

type collectionDownloadRepository interface {
	Create(download *model.Download) error
	GetByHash(hash string) (*model.Download, error)
}

// NewCollectionDownloader 创建合集下载服务实例
func NewCollectionDownloader(
	qbClient downloader.QBittorrentClient,
	downloadRepo collectionDownloadRepository,
	configRepo repository.ConfigRepository,
	basePath string,
) CollectionDownloader {
	return &collectionDownloader{
		qbClient:     qbClient,
		downloadRepo: downloadRepo,
		configRepo:   configRepo,
		basePath:     basePath,
	}
}

func (c *collectionDownloader) Download(subscription *model.Subscription) (*model.Download, error) {
	// 检查是否有合集种子
	if subscription.CollectionTorrent == "" {
		return nil, nil
	}

	// 检查qBittorrent客户端
	if c.qbClient == nil {
		logger.Warn("qBittorrent client not configured, skipping collection torrent download",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name)
		return nil, nil
	}

	logger.Info("Starting collection torrent download",
		"subscription_id", subscription.ID,
		"subscription_name", subscription.Name,
		"collection_torrent", subscription.CollectionTorrent)

	// 生成带番剧名的下载路径
	downloadPath := utils.GenerateDownloadPath(c.basePath, subscription.Name)

	// 验证生成的下载路径不会逃逸出基础下载目录（防止路径遍历）
	if _, err := utils.ValidatePath(downloadPath, c.basePath); err != nil {
		logger.Error("Generated download path escapes base directory",
			"subscription", subscription.Name,
			"download_path", downloadPath,
			"error", err)
		return nil, err
	}

	var torrentHash string
	var err error

	// 检查是否是 .torrent URL，需要通过代理下载
	torrentURL := subscription.CollectionTorrent
	if strings.HasSuffix(strings.ToLower(torrentURL), ".torrent") ||
		strings.Contains(torrentURL, "/Download/") {
		// 设置代理
		if c.configRepo != nil {
			if proxyConfig, err := c.configRepo.Get("system_proxy"); err == nil && proxyConfig != nil && proxyConfig.Value != "" {
				c.qbClient.SetProxy(proxyConfig.Value)
			}
		}

		// 先下载种子文件
		fileContent, downloadErr := c.qbClient.DownloadTorrentFile(torrentURL)
		if downloadErr != nil {
			logger.Error("Failed to download collection torrent file",
				"subscription_id", subscription.ID,
				"torrent_url", torrentURL,
				"error", downloadErr)
			return nil, downloadErr
		}

		// 通过文件内容添加种子
		torrentHash, err = c.qbClient.AddTorrentFile(
			"collection.torrent",
			fileContent,
			downloadPath,
			downloader.AutoRssCategory,
		)
	} else {
		// magnet 链接或其他，直接添加
		torrentHash, err = c.qbClient.AddTorrent(
			torrentURL,
			downloadPath,
			downloader.AutoRssCategory,
		)
	}

	if err != nil {
		logger.Error("Failed to add collection torrent to qBittorrent",
			"subscription_id", subscription.ID,
			"subscription_name", subscription.Name,
			"torrent_url", torrentURL,
			"download_path", downloadPath,
			"error", err)
		return nil, err
	}

	// 如果没有获取到 hash（种子可能已存在），尝试通过 savePath 查找
	if torrentHash == "" {
		logger.Info("Torrent hash empty, searching for existing torrent by savePath",
			"subscription_id", subscription.ID,
			"download_path", downloadPath)

		torrents, listErr := c.qbClient.GetTorrentsByCategory(downloader.AutoRssCategory)
		if listErr == nil {
			for _, t := range torrents {
				// 匹配 savePath（可能完全匹配或以 downloadPath 开头）
				if t.SavePath == downloadPath || strings.HasPrefix(t.SavePath, downloadPath) {
					torrentHash = t.Hash
					logger.Info("Found existing torrent by savePath",
						"subscription_id", subscription.ID,
						"torrent_hash", torrentHash,
						"torrent_name", t.Name,
						"save_path", t.SavePath)
					break
				}
			}
		}
	}

	logger.Info("Collection torrent added successfully",
		"subscription_id", subscription.ID,
		"subscription_name", subscription.Name,
		"torrent_hash", torrentHash,
		"download_path", downloadPath)

	// 创建 Download 记录以支持自动重命名
	if torrentHash != "" && c.downloadRepo != nil {
		// 先检查是否已存在相同 hash 的记录
		existing, _ := c.downloadRepo.GetByHash(torrentHash)
		if existing != nil {
			logger.Info("Download record already exists for collection torrent",
				"subscription_id", subscription.ID,
				"torrent_hash", torrentHash)
			return existing, nil
		}

		download := &model.Download{
			SubscriptionID: subscription.ID,
			Title:          subscription.Name + " [合集]",
			Episode:        0, // 0 表示合集
			Fansub:         subscription.Fansub,
			TorrentURL:     torrentURL,
			TorrentHash:    torrentHash,
			Status:         "downloading",
		}

		if err := c.downloadRepo.Create(download); err != nil {
			logger.Error("Failed to create download record for collection torrent",
				"subscription_id", subscription.ID,
				"torrent_hash", torrentHash,
				"error", err)
			return nil, err
		}

		logger.Info("Download record created for collection torrent",
			"subscription_id", subscription.ID,
			"download_id", download.ID,
			"torrent_hash", torrentHash)

		return download, nil
	}

	return nil, nil
}

func (c *collectionDownloader) DownloadAsync(subscription *model.Subscription) {
	go func() {
		if _, err := c.Download(subscription); err != nil {
			logger.Error("Async collection download failed",
				"subscription_id", subscription.ID,
				"error", err.Error())
		}
	}()
}
