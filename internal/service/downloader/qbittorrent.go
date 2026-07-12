package downloader

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/utils"
	qbclient "github.com/WormW/auto-rss/internal/qbittorrent"
	"github.com/go-resty/resty/v2"
)

const (
	qbRequestTimeout       = 30 * time.Second
	torrentAddPollTimeout  = 5 * time.Second
	torrentAddPollInterval = 500 * time.Millisecond
)

type QBittorrentClient = qbclient.Client
type TorrentInfo = qbclient.TorrentInfo
type TorrentFile = qbclient.TorrentFile

var ErrTorrentAlreadyExists = qbclient.ErrTorrentAlreadyExists
var ErrTorrentNotFound = qbclient.ErrTorrentNotFound
var ErrTorrentOwnershipUnconfirmed = qbclient.ErrTorrentOwnershipUnconfirmed

type qbittorrentClient struct {
	host        string
	username    string
	password    string
	cookie      string
	client      *resty.Client
	proxyClient *resty.Client // 用于通过代理下载种子文件
}

// NewQBittorrentClient 创建 qBittorrent 客户端实例
func NewQBittorrentClient() QBittorrentClient {
	client := resty.New()
	client.SetTimeout(qbRequestTimeout)
	// 启用 cookie jar 以保存和发送 cookies
	client.SetCookieJar(nil) // nil 使用默认的 cookie jar

	proxyClient := resty.New()
	proxyClient.SetTimeout(qbRequestTimeout)

	return &qbittorrentClient{
		client:      client,
		proxyClient: proxyClient,
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
		return fmt.Errorf("login failed: status code %d, body: %s", resp.StatusCode(), string(resp.Body()))
	}

	// 检查响应内容
	body := string(resp.Body())
	if body != "Ok." {
		return fmt.Errorf("login failed: %s", body)
	}

	// 保存 cookie 并设置为默认 header
	cookies := resp.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "SID" {
			c.cookie = cookie.Value
			// 在所有后续请求中添加 Cookie header
			c.client.SetHeader("Cookie", fmt.Sprintf("SID=%s", cookie.Value))
			break
		}
	}

	if c.cookie == "" {
		return fmt.Errorf("no SID cookie received from qBittorrent")
	}

	return nil
}

// AddTorrent 添加种子任务
func (c *qbittorrentClient) AddTorrent(torrentURL string, savePath string, category string) (string, error) {
	// 尝试从 URL 中提取 hash（支持 magnet link）
	extractedHash := utils.ExtractHashFromURL(torrentURL)

	// 获取添加前的种子列表，用于后续识别新添加的种子
	existingTorrents, err := c.GetTorrentsByCategory(category)
	if err != nil {
		// 如果获取失败，不影响添加操作
		existingTorrents = []*TorrentInfo{}
	}
	existingHashes := make(map[string]bool)
	for _, t := range existingTorrents {
		existingHashes[strings.ToLower(t.Hash)] = true
	}

	// 如果能提取到 hash，检查种子是否已存在
	if extractedHash != "" {
		if existingHashes[strings.ToLower(extractedHash)] {
			// 种子已存在，直接返回 hash
			return strings.ToLower(extractedHash), nil
		}
	}

	formData := map[string]string{
		"urls":     torrentURL,
		"savepath": savePath,
	}

	// 如果指定了分类，则设置分类
	if category != "" {
		formData["category"] = category
	}

	resp, err := c.client.R().
		SetFormData(formData).
		Post(c.host + "/api/v2/torrents/add")

	if err != nil {
		return "", fmt.Errorf("add torrent request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("add torrent failed: status code %d, body: %s", resp.StatusCode(), string(resp.Body()))
	}

	if hash := c.waitForAddedTorrentHash(category, existingHashes, extractedHash); hash != "" {
		return hash, nil
	}

	// 对于 mikan 的 .torrent URL，可从链接中提取 40 位 info-hash，避免返回空 hash。
	if guessedHash := utils.ExtractInfoHashFromTorrentURL(torrentURL); guessedHash != "" {
		return guessedHash, nil
	}

	// 没有找到 hash，但种子可能已成功添加，返回空字符串（后续 monitor 会处理）
	return "", nil
}

// AddTorrentExclusive adds a torrent only when this call can prove ownership
// of a task that did not exist in the global pre-add snapshot.
func (c *qbittorrentClient) AddTorrentExclusive(torrentURL, savePath, category, expectedHash string) (string, error) {
	expectedSavePath, err := normalizedTorrentSavePath(savePath)
	if err != nil {
		return "", fmt.Errorf("exclusive add save path: %w", err)
	}
	before, err := c.getAllTorrents()
	if err != nil {
		return "", fmt.Errorf("snapshot torrents before exclusive add: %w", err)
	}
	existing := make(map[string]struct{}, len(before))
	for _, torrent := range before {
		hash := strings.ToLower(strings.TrimSpace(torrent.Hash))
		if hash != "" {
			existing[hash] = struct{}{}
		}
	}
	expectedHash = strings.ToLower(strings.TrimSpace(expectedHash))
	if expectedHash == "" {
		expectedHash = strings.ToLower(strings.TrimSpace(utils.ExtractHashFromURL(torrentURL)))
	}
	if expectedHash != "" {
		if _, found := existing[expectedHash]; found {
			return "", fmt.Errorf("%w: %s", ErrTorrentAlreadyExists, expectedHash)
		}
	}

	formData := map[string]string{"urls": torrentURL, "savepath": savePath}
	if category != "" {
		formData["category"] = category
	}
	resp, err := c.client.R().SetFormData(formData).Post(c.host + "/api/v2/torrents/add")
	if err != nil {
		return "", fmt.Errorf("%w: exclusive add torrent request failed: %v", ErrTorrentOwnershipUnconfirmed, err)
	}
	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("%w: exclusive add torrent failed: status code %d, body: %s", ErrTorrentOwnershipUnconfirmed, resp.StatusCode(), string(resp.Body()))
	}

	deadline := time.Now().Add(torrentAddPollTimeout)
	for {
		after, snapshotErr := c.getAllTorrents()
		if snapshotErr == nil {
			owned := make([]string, 0, 1)
			for _, torrent := range after {
				hash := strings.ToLower(strings.TrimSpace(torrent.Hash))
				actualSavePath, pathErr := normalizedTorrentSavePath(torrent.SavePath)
				if hash == "" || torrent.Category != category || pathErr != nil || actualSavePath != expectedSavePath {
					continue
				}
				if _, preexisting := existing[hash]; preexisting {
					continue
				}
				if expectedHash != "" && hash != expectedHash {
					continue
				}
				owned = append(owned, hash)
			}
			if len(owned) == 1 {
				return owned[0], nil
			}
			if len(owned) > 1 {
				return "", fmt.Errorf("%w: exclusive add ownership is ambiguous", ErrTorrentOwnershipUnconfirmed)
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%w: exclusive add ownership was not confirmed", ErrTorrentOwnershipUnconfirmed)
		}
		time.Sleep(torrentAddPollInterval)
	}
}

func normalizedTorrentSavePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("save path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// getAllTorrents 获取所有种子
func (c *qbittorrentClient) getAllTorrents() ([]*TorrentInfo, error) {
	var torrents []map[string]interface{}

	resp, err := c.client.R().
		SetResult(&torrents).
		Get(c.host + "/api/v2/torrents/info")

	if err != nil {
		return nil, fmt.Errorf("get torrents request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("get torrents failed: status code %d", resp.StatusCode())
	}

	result := make([]*TorrentInfo, 0, len(torrents))
	for _, t := range torrents {
		info := &TorrentInfo{
			Hash:     strings.ToLower(fmt.Sprintf("%v", t["hash"])),
			Name:     fmt.Sprintf("%v", t["name"]),
			State:    fmt.Sprintf("%v", t["state"]),
			SavePath: fmt.Sprintf("%v", t["save_path"]),
			Category: fmt.Sprintf("%v", t["category"]),
		}
		if progress, ok := t["progress"].(float64); ok {
			info.Progress = progress
		}
		if size, ok := t["size"].(float64); ok {
			info.Size = int64(size)
		}
		result = append(result, info)
	}

	return result, nil
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
		return nil, ErrTorrentNotFound
	}

	torrent := torrents[0]
	info := &TorrentInfo{
		Hash:       getStringValue(torrent, "hash"),
		Name:       getStringValue(torrent, "name"),
		Progress:   getFloatValue(torrent, "progress"),
		State:      getStringValue(torrent, "state"),
		SavePath:   getStringValue(torrent, "save_path"),
		Category:   getStringValue(torrent, "category"),
		Size:       getInt64Value(torrent, "size"),
		Downloaded: getInt64Value(torrent, "downloaded"),
		Uploaded:   getInt64Value(torrent, "uploaded"),
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

// 辅助函数：从 map 中安全获取 int64 值
func getInt64Value(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int64:
			return val
		case float64:
			return int64(val)
		case int:
			return int64(val)
		}
	}
	return 0
}

// RemoveTorrentTask 仅删除 qBittorrent 任务，不删除已下载文件。
func (c *qbittorrentClient) RemoveTorrentTask(hash string) error {
	return c.deleteTorrent(hash, false)
}

// PauseTorrent 暂停指定种子任务。
func (c *qbittorrentClient) PauseTorrent(hash string) error {
	return c.controlTorrent(hash, "pause")
}

// ResumeTorrent 恢复指定种子任务。
func (c *qbittorrentClient) ResumeTorrent(hash string) error {
	return c.controlTorrent(hash, "resume")
}

func (c *qbittorrentClient) controlTorrent(hash, action string) error {
	resp, err := c.client.R().
		SetFormData(map[string]string{"hashes": hash}).
		Post(c.host + "/api/v2/torrents/" + action)
	if err != nil {
		return fmt.Errorf("%s torrent request failed: %w", action, err)
	}
	if resp.StatusCode() != 200 {
		return fmt.Errorf("%s torrent failed: status code %d", action, resp.StatusCode())
	}
	return nil
}

// DeleteTorrentWithPayload 删除 qBittorrent 任务及其已下载文件。
func (c *qbittorrentClient) DeleteTorrentWithPayload(hash string) error {
	return c.deleteTorrent(hash, true)
}

func (c *qbittorrentClient) deleteTorrent(hash string, deleteFiles bool) error {
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

// TestConnection 测试连接
func (c *qbittorrentClient) TestConnection(host, username, password string) error {
	// 创建临时客户端用于测试
	testClient := resty.New()
	testClient.SetTimeout(qbRequestTimeout)
	host = strings.TrimSuffix(host, "/")

	// 尝试登录
	resp, err := testClient.R().
		SetFormData(map[string]string{
			"username": username,
			"password": password,
		}).
		Post(host + "/api/v2/auth/login")

	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}

	if resp.StatusCode() == 403 {
		return fmt.Errorf("认证失败: 用户名或密码错误")
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("连接失败: HTTP状态码 %d", resp.StatusCode())
	}

	// 检查响应内容
	body := string(resp.Body())
	if body == "Fails." {
		return fmt.Errorf("认证失败: 用户名或密码错误")
	}

	// 获取cookie
	var sid string
	cookies := resp.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "SID" {
			sid = cookie.Value
			break
		}
	}

	if sid == "" {
		return fmt.Errorf("连接失败: 未获取到会话令牌")
	}

	// 测试API调用 - 获取版本信息
	testClient.SetHeader("Cookie", "SID="+sid)

	versionResp, err := testClient.R().
		Get(host + "/api/v2/app/version")

	if err != nil {
		return fmt.Errorf("API测试失败: %w", err)
	}

	if versionResp.StatusCode() != 200 {
		return fmt.Errorf("API测试失败: HTTP状态码 %d", versionResp.StatusCode())
	}

	return nil
}

// GetVersion 获取qBittorrent版本信息
func (c *qbittorrentClient) GetVersion() (string, error) {
	resp, err := c.client.R().
		Get(c.host + "/api/v2/app/version")

	if err != nil {
		return "", fmt.Errorf("get version request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("get version failed: status code %d", resp.StatusCode())
	}

	return string(resp.Body()), nil
}

// GetTorrentsByCategory 获取指定分类的所有种子
func (c *qbittorrentClient) GetTorrentsByCategory(category string) ([]*TorrentInfo, error) {
	var torrents []map[string]interface{}

	req := c.client.R().SetResult(&torrents)

	// 如果指定了分类，添加过滤参数
	if category != "" {
		req.SetQueryParam("category", category)
	}

	resp, err := req.Get(c.host + "/api/v2/torrents/info")

	if err != nil {
		return nil, fmt.Errorf("get torrents request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("get torrents failed: status code %d", resp.StatusCode())
	}

	var result []*TorrentInfo
	for _, torrent := range torrents {
		info := &TorrentInfo{
			Hash:       getStringValue(torrent, "hash"),
			Name:       getStringValue(torrent, "name"),
			Progress:   getFloatValue(torrent, "progress"),
			State:      getStringValue(torrent, "state"),
			SavePath:   getStringValue(torrent, "save_path"),
			Category:   getStringValue(torrent, "category"),
			Size:       getInt64Value(torrent, "size"),
			Downloaded: getInt64Value(torrent, "downloaded"),
			Uploaded:   getInt64Value(torrent, "uploaded"),
		}
		result = append(result, info)
	}

	return result, nil
}

// SetCategory 设置种子分类
func (c *qbittorrentClient) SetCategory(hash string, category string) error {
	resp, err := c.client.R().
		SetFormData(map[string]string{
			"hashes":   hash,
			"category": category,
		}).
		Post(c.host + "/api/v2/torrents/setCategory")

	if err != nil {
		return fmt.Errorf("set category request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("set category failed: status code %d", resp.StatusCode())
	}

	return nil
}

// SetLocation 移动种子到新位置
func (c *qbittorrentClient) SetLocation(hash string, location string) error {
	resp, err := c.client.R().
		SetFormData(map[string]string{
			"hashes":   hash,
			"location": location,
		}).
		Post(c.host + "/api/v2/torrents/setLocation")

	if err != nil {
		return fmt.Errorf("set location request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("set location failed: status code %d, body: %s", resp.StatusCode(), string(resp.Body()))
	}

	return nil
}

// RenameTorrentFile 重命名种子文件
func (c *qbittorrentClient) RenameTorrentFile(hash string, oldPath string, newPath string) error {
	resp, err := c.client.R().
		SetFormData(map[string]string{
			"hash":    hash,
			"oldPath": oldPath,
			"newPath": newPath,
		}).
		Post(c.host + "/api/v2/torrents/renameFile")

	if err != nil {
		return fmt.Errorf("rename file request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("rename file failed: status code %d", resp.StatusCode())
	}

	return nil
}

// SetProxy 设置代理用于下载种子文件
func (c *qbittorrentClient) SetProxy(proxyURL string) error {
	if proxyURL == "" {
		c.proxyClient.RemoveProxy()
		return nil
	}
	c.proxyClient.SetProxy(proxyURL)
	return nil
}

// AddTorrentFile 通过文件内容添加种子
func (c *qbittorrentClient) AddTorrentFile(filename string, fileContent []byte, savePath string, category string) (string, error) {
	// 获取添加前的种子列表
	existingTorrents, _ := c.GetTorrentsByCategory(category)
	existingHashes := make(map[string]bool)
	for _, t := range existingTorrents {
		existingHashes[strings.ToLower(t.Hash)] = true
	}

	formData := map[string]string{
		"savepath": savePath,
	}
	if category != "" {
		formData["category"] = category
	}

	// 使用multipart/form-data上传文件
	req := c.client.R().
		SetFileReader("torrents", filename, bytes.NewReader(fileContent)).
		SetFormData(formData)

	resp, err := req.Post(c.host + "/api/v2/torrents/add")
	if err != nil {
		return "", fmt.Errorf("add torrent file request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("add torrent file failed: status code %d, body: %s", resp.StatusCode(), string(resp.Body()))
	}

	if hash := c.waitForAddedTorrentHash(category, existingHashes, ""); hash != "" {
		return hash, nil
	}

	return "", nil
}

func (c *qbittorrentClient) waitForAddedTorrentHash(category string, existingHashes map[string]bool, fallbackHash string) string {
	deadline := time.Now().Add(torrentAddPollTimeout)

	for {
		torrents, err := c.GetTorrentsByCategory(category)
		if err != nil {
			torrents, err = c.getAllTorrents()
		}

		if err == nil {
			for _, t := range torrents {
				hash := strings.ToLower(strings.TrimSpace(t.Hash))
				if hash == "" {
					continue
				}
				if !existingHashes[hash] {
					return hash
				}
			}

			if fallbackHash != "" {
				if torrentInfo, err := c.GetTorrentInfo(fallbackHash); err == nil && torrentInfo != nil {
					if category != "" {
						_ = c.SetCategory(fallbackHash, category)
					}
					return strings.ToLower(torrentInfo.Hash)
				}
			}
		}

		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(torrentAddPollInterval)
	}
}

// DownloadTorrentFile 通过代理下载种子文件
func (c *qbittorrentClient) DownloadTorrentFile(url string) ([]byte, error) {
	resp, err := c.proxyClient.R().Get(url)
	if err != nil {
		return nil, fmt.Errorf("download torrent file failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("download torrent file failed: status code %d", resp.StatusCode())
	}

	return resp.Body(), nil
}
