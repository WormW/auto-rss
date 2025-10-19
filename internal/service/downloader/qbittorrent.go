package downloader

import (
	"fmt"
	"strings"

	"github.com/go-resty/resty/v2"
)

// QBittorrentClient qBittorrent 客户端接口
type QBittorrentClient interface {
	// Login 登录
	Login(host, username, password string) error
	// AddTorrent 添加种子任务
	AddTorrent(torrentURL string, savePath string) (string, error)
	// GetTorrentInfo 获取种子信息
	GetTorrentInfo(hash string) (*TorrentInfo, error)
	// DeleteTorrent 删除种子任务
	DeleteTorrent(hash string, deleteFiles bool) error
	// GetTorrentFiles 获取种子文件列表
	GetTorrentFiles(hash string) ([]TorrentFile, error)
}

// TorrentInfo 种子信息
type TorrentInfo struct {
	Hash     string
	Name     string
	Progress float64
	Status   string
	SavePath string
}

// TorrentFile 种子文件
type TorrentFile struct {
	Name     string
	Size     int64
	Progress float64
}

type qbittorrentClient struct {
	host     string
	username string
	password string
	cookie   string
	client   *resty.Client
}

// NewQBittorrentClient 创建 qBittorrent 客户端实例
func NewQBittorrentClient() QBittorrentClient {
	return &qbittorrentClient{
		client: resty.New(),
	}
}

// Login 登录
func (c *qbittorrentClient) Login(host, username, password string) error {
	c.host = strings.TrimSuffix(host, "/")
	c.username = username
	c.password = password

	resp, err := c.client.R().
		SetFormData(map[string]string{
			"username": username,
			"password": password,
		}).
		Post(c.host + "/api/v2/auth/login")

	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("login failed: status code %d", resp.StatusCode())
	}

	// 保存 cookie
	cookies := resp.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "SID" {
			c.cookie = cookie.Value
			c.client.SetCookie(&resty.Cookie{
				Name:  "SID",
				Value: cookie.Value,
			})
			break
		}
	}

	return nil
}

// AddTorrent 添加种子任务
func (c *qbittorrentClient) AddTorrent(torrentURL string, savePath string) (string, error) {
	resp, err := c.client.R().
		SetFormData(map[string]string{
			"urls":     torrentURL,
			"savepath": savePath,
		}).
		Post(c.host + "/api/v2/torrents/add")

	if err != nil {
		return "", fmt.Errorf("add torrent request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("add torrent failed: status code %d", resp.StatusCode())
	}

	// qBittorrent API 返回 "Ok." 表示成功
	// 需要后续通过种子 URL 或名称来查询实际的 hash
	return "", nil
}

// GetTorrentInfo 获取种子信息
func (c *qbittorrentClient) GetTorrentInfo(hash string) (*TorrentInfo, error) {
	var torrents []map[string]interface{}

	resp, err := c.client.R().
		SetResult(&torrents).
		SetQueryParam("hashes", hash).
		Get(c.host + "/api/v2/torrents/info")

	if err != nil {
		return nil, fmt.Errorf("get torrent info request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("get torrent info failed: status code %d", resp.StatusCode())
	}

	if len(torrents) == 0 {
		return nil, fmt.Errorf("torrent not found")
	}

	torrent := torrents[0]
	info := &TorrentInfo{
		Hash:     getStringValue(torrent, "hash"),
		Name:     getStringValue(torrent, "name"),
		Progress: getFloatValue(torrent, "progress"),
		Status:   getStringValue(torrent, "state"),
		SavePath: getStringValue(torrent, "save_path"),
	}

	return info, nil
}

// 辅助函数：从 map 中安全获取字符串值
func getStringValue(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if str, ok := v.(string); ok {
			return str
		}
	}
	return ""
}

// 辅助函数：从 map 中安全获取浮点值
func getFloatValue(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

// DeleteTorrent 删除种子任务
func (c *qbittorrentClient) DeleteTorrent(hash string, deleteFiles bool) error {
	resp, err := c.client.R().
		SetFormData(map[string]string{
			"hashes":      hash,
			"deleteFiles": fmt.Sprintf("%t", deleteFiles),
		}).
		Post(c.host + "/api/v2/torrents/delete")

	if err != nil {
		return fmt.Errorf("delete torrent request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("delete torrent failed: status code %d", resp.StatusCode())
	}

	return nil
}

// GetTorrentFiles 获取种子文件列表
func (c *qbittorrentClient) GetTorrentFiles(hash string) ([]TorrentFile, error) {
	var files []map[string]interface{}

	resp, err := c.client.R().
		SetResult(&files).
		SetQueryParam("hash", hash).
		Get(c.host + "/api/v2/torrents/files")

	if err != nil {
		return nil, fmt.Errorf("get torrent files request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("get torrent files failed: status code %d", resp.StatusCode())
	}

	var torrentFiles []TorrentFile
	for _, file := range files {
		torrentFile := TorrentFile{
			Name:     getStringValue(file, "name"),
			Size:     int64(getFloatValue(file, "size")),
			Progress: getFloatValue(file, "progress"),
		}
		torrentFiles = append(torrentFiles, torrentFile)
	}

	return torrentFiles, nil
}
