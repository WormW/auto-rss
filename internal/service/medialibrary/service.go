package medialibrary

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/go-resty/resty/v2"
)

const (
	ConfigKey = "media_library_config"

	ProviderJellyfin = "jellyfin"
	ProviderEmby     = "emby"
	ProviderPlex     = "plex"

	RefreshStatusPending  = "pending"
	RefreshStatusDisabled = "disabled"
	RefreshStatusSuccess  = "success"
	RefreshStatusFailed   = "failed"
)

// PathMapping maps a local download path prefix to the path seen by the media library server.
type PathMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Config is the persisted media library integration configuration.
type Config struct {
	Enabled         bool          `json:"enabled"`
	Provider        string        `json:"provider"`
	BaseURL         string        `json:"base_url"`
	Token           string        `json:"token"`
	Username        string        `json:"username,omitempty"`
	Password        string        `json:"password,omitempty"`
	LibraryID       string        `json:"library_id,omitempty"`
	SectionID       string        `json:"section_id,omitempty"`
	PathMappings    []PathMapping `json:"path_mappings"`
	RefreshOnImport bool          `json:"refresh_on_import"`
}

// ConfigResponse hides fields that should not be echoed to the browser.
type ConfigResponse struct {
	Enabled         bool          `json:"enabled"`
	Provider        string        `json:"provider"`
	BaseURL         string        `json:"base_url"`
	TokenConfigured bool          `json:"token_configured"`
	Username        string        `json:"username,omitempty"`
	LibraryID       string        `json:"library_id,omitempty"`
	SectionID       string        `json:"section_id,omitempty"`
	PathMappings    []PathMapping `json:"path_mappings"`
	RefreshOnImport bool          `json:"refresh_on_import"`
}

// RefreshResult captures the durable result of one refresh attempt.
type RefreshResult struct {
	Enabled     bool       `json:"enabled"`
	Status      string     `json:"status"`
	Message     string     `json:"message"`
	Path        string     `json:"path"`
	RefreshedAt *time.Time `json:"refreshed_at,omitempty"`
}

// Service manages media library configuration, path mapping, and refresh operations.
type Service struct {
	configRepo   repository.ConfigRepository
	downloadRepo repository.DownloadRepository
	client       *resty.Client
}

// NewService creates a media library service.
func NewService(configRepo repository.ConfigRepository, downloadRepo repository.DownloadRepository) *Service {
	client := resty.New().
		SetTimeout(15*time.Second).
		SetHeader("User-Agent", "Auto-RSS/1.0")

	return &Service{
		configRepo:   configRepo,
		downloadRepo: downloadRepo,
		client:       client,
	}
}

// GetConfig returns the stored media library config, or a disabled default.
func (s *Service) GetConfig() (Config, error) {
	cfg := DefaultConfig()
	if s == nil || s.configRepo == nil {
		return cfg, nil
	}

	record, err := s.configRepo.Get(ConfigKey)
	if err != nil || record == nil || strings.TrimSpace(record.Value) == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(record.Value), &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid media library config: %w", err)
	}
	normalizeConfig(&cfg)
	return cfg, nil
}

// SaveConfig validates and stores a media library config.
func (s *Service) SaveConfig(cfg Config) error {
	if s == nil || s.configRepo == nil {
		return errors.New("config repository is not available")
	}
	normalizeConfig(&cfg)
	if cfg.Token == "" {
		if existing, err := s.GetConfig(); err == nil && existing.Token != "" {
			cfg.Token = existing.Token
		}
	}
	if err := ValidateConfig(cfg); err != nil {
		return err
	}

	payload, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.configRepo.Set(ConfigKey, string(payload))
}

// PublicConfig converts a full config into a response safe for API clients.
func PublicConfig(cfg Config) ConfigResponse {
	return ConfigResponse{
		Enabled:         cfg.Enabled,
		Provider:        cfg.Provider,
		BaseURL:         cfg.BaseURL,
		TokenConfigured: strings.TrimSpace(cfg.Token) != "",
		Username:        cfg.Username,
		LibraryID:       cfg.LibraryID,
		SectionID:       cfg.SectionID,
		PathMappings:    cfg.PathMappings,
		RefreshOnImport: cfg.RefreshOnImport,
	}
}

// DefaultConfig returns the default disabled Jellyfin-compatible config.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		Provider:        ProviderJellyfin,
		RefreshOnImport: true,
		PathMappings:    []PathMapping{},
	}
}

// TestConnection validates the config by issuing a provider-specific lightweight request.
func (s *Service) TestConnection(cfg Config) error {
	normalizeConfig(&cfg)
	cfg.Enabled = true
	if cfg.Token == "" {
		if existing, err := s.GetConfig(); err == nil && existing.Token != "" {
			cfg.Token = existing.Token
		}
	}
	if err := ValidateConfig(cfg); err != nil {
		return err
	}

	switch cfg.Provider {
	case ProviderPlex:
		return s.testPlex(cfg)
	case ProviderJellyfin, ProviderEmby:
		return s.testJellyfinCompatible(cfg)
	default:
		return fmt.Errorf("unsupported media library provider: %s", cfg.Provider)
	}
}

// RefreshDownload maps a download path and triggers media library refresh if enabled.
func (s *Service) RefreshDownload(download *model.Download) RefreshResult {
	return s.refreshDownload(download, false)
}

// RefreshDownloadAfterImport refreshes a download only when automatic import refresh is enabled.
func (s *Service) RefreshDownloadAfterImport(download *model.Download) RefreshResult {
	return s.refreshDownload(download, true)
}

func (s *Service) refreshDownload(download *model.Download, respectAutoRefresh bool) RefreshResult {
	result := RefreshResult{Status: RefreshStatusDisabled, Message: "媒体库刷新未启用"}
	if s == nil || download == nil {
		result.Status = RefreshStatusFailed
		result.Message = "下载记录不可用"
		return result
	}

	cfg, err := s.GetConfig()
	if err != nil {
		result.Status = RefreshStatusFailed
		result.Message = err.Error()
		s.applyRefreshResult(download, result)
		return result
	}
	if !cfg.Enabled || (respectAutoRefresh && !cfg.RefreshOnImport) {
		if cfg.Enabled {
			result.Message = "整理完成后自动刷新未启用"
		}
		result.Enabled = cfg.Enabled
		s.applyRefreshResult(download, result)
		return result
	}

	sourcePath := ResolveDownloadPath(download)
	mappedPath, err := MapPath(sourcePath, cfg.PathMappings)
	if err != nil {
		result.Enabled = true
		result.Status = RefreshStatusFailed
		result.Message = err.Error()
		result.Path = sourcePath
		s.applyRefreshResult(download, result)
		return result
	}

	result.Enabled = true
	result.Path = mappedPath
	now := time.Now()
	if err := s.RefreshPath(cfg, mappedPath); err != nil {
		result.Status = RefreshStatusFailed
		result.Message = err.Error()
		result.RefreshedAt = &now
		s.applyRefreshResult(download, result)
		return result
	}

	result.Status = RefreshStatusSuccess
	result.Message = "媒体库刷新已触发"
	result.RefreshedAt = &now
	s.applyRefreshResult(download, result)
	return result
}

// RefreshPath triggers a provider refresh. Path is currently recorded and used by providers
// that can scope scans; Jellyfin/Emby library refresh is library-wide.
func (s *Service) RefreshPath(cfg Config, libraryPath string) error {
	normalizeConfig(&cfg)
	if !cfg.Enabled {
		return errors.New("media library integration is disabled")
	}
	if err := ValidateConfig(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(libraryPath) == "" {
		return errors.New("media library path is empty")
	}

	switch cfg.Provider {
	case ProviderPlex:
		return s.refreshPlex(cfg)
	case ProviderJellyfin, ProviderEmby:
		return s.refreshJellyfinCompatible(cfg)
	default:
		return fmt.Errorf("unsupported media library provider: %s", cfg.Provider)
	}
}

func (s *Service) applyRefreshResult(download *model.Download, result RefreshResult) {
	download.MediaLibraryPath = result.Path
	download.MediaLibraryRefreshStatus = result.Status
	download.MediaLibraryRefreshError = ""
	if result.Status == RefreshStatusFailed {
		download.MediaLibraryRefreshError = result.Message
	}
	download.MediaLibraryRefreshedAt = result.RefreshedAt

	if s.downloadRepo != nil && download.ID != 0 {
		if err := s.downloadRepo.Update(download); err != nil {
			logger.Warn("Failed to persist media library refresh result",
				"download_id", download.ID,
				"status", result.Status,
				"error", err.Error())
		}
	}
}

func (s *Service) testJellyfinCompatible(cfg Config) error {
	endpoint := joinURL(cfg.BaseURL, "/System/Info/Public")
	resp, err := s.client.R().
		SetHeader("X-Emby-Token", cfg.Token).
		Get(endpoint)
	if err != nil {
		return fmt.Errorf("media library connection failed: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("media library connection failed: status %d", resp.StatusCode())
	}
	return nil
}

func (s *Service) testPlex(cfg Config) error {
	endpoint := joinURL(cfg.BaseURL, "/identity")
	resp, err := s.client.R().
		SetQueryParam("X-Plex-Token", cfg.Token).
		Get(endpoint)
	if err != nil {
		return fmt.Errorf("media library connection failed: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("media library connection failed: status %d", resp.StatusCode())
	}
	return nil
}

func (s *Service) refreshJellyfinCompatible(cfg Config) error {
	endpoint := joinURL(cfg.BaseURL, "/Library/Refresh")
	req := s.client.R().SetHeader("X-Emby-Token", cfg.Token)
	if cfg.LibraryID != "" {
		req.SetQueryParam("collectionType", cfg.LibraryID)
	}

	resp, err := req.Post(endpoint)
	if err != nil {
		return fmt.Errorf("media library refresh failed: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("media library refresh failed: status %d", resp.StatusCode())
	}
	return nil
}

func (s *Service) refreshPlex(cfg Config) error {
	if cfg.SectionID == "" {
		return errors.New("plex section_id is required for refresh")
	}
	endpoint := joinURL(cfg.BaseURL, "/library/sections/"+url.PathEscape(cfg.SectionID)+"/refresh")
	resp, err := s.client.R().
		SetQueryParam("X-Plex-Token", cfg.Token).
		Get(endpoint)
	if err != nil {
		return fmt.Errorf("media library refresh failed: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("media library refresh failed: status %d", resp.StatusCode())
	}
	return nil
}

// ResolveDownloadPath returns the final local path for a download.
func ResolveDownloadPath(download *model.Download) string {
	if download == nil {
		return ""
	}
	if strings.TrimSpace(download.RenamedPath) != "" {
		return strings.TrimSpace(download.RenamedPath)
	}
	return strings.TrimSpace(download.FilePath)
}

// MapPath applies the longest matching path mapping to localPath.
func MapPath(localPath string, mappings []PathMapping) (string, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return "", errors.New("download path is empty")
	}
	if len(mappings) == 0 {
		return localPath, nil
	}

	cleanLocal := normalizePath(localPath)
	var best *PathMapping
	bestFrom := ""
	for i := range mappings {
		from := normalizePath(mappings[i].From)
		if from == "" {
			continue
		}
		if pathHasPrefix(cleanLocal, from) && len(from) > len(bestFrom) {
			best = &mappings[i]
			bestFrom = from
		}
	}
	if best == nil {
		return "", fmt.Errorf("media library path mapping not found for %s", localPath)
	}

	to := strings.TrimSpace(best.To)
	if to == "" {
		return "", fmt.Errorf("media library path mapping target is empty for %s", best.From)
	}

	rel := strings.TrimPrefix(cleanLocal, bestFrom)
	rel = strings.TrimLeft(rel, `/\`)
	if rel == "" {
		return strings.TrimRight(to, `/\`), nil
	}
	return joinMappedPath(to, rel), nil
}

// ValidateConfig validates a media library config.
func ValidateConfig(cfg Config) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.Provider == "" {
		return errors.New("provider is required")
	}
	switch cfg.Provider {
	case ProviderJellyfin, ProviderEmby, ProviderPlex:
	default:
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
	if cfg.BaseURL == "" {
		return errors.New("base_url is required")
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("base_url must be a valid absolute URL")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return errors.New("token is required")
	}
	if cfg.Provider == ProviderPlex && strings.TrimSpace(cfg.SectionID) == "" {
		return errors.New("section_id is required for Plex")
	}
	for _, mapping := range cfg.PathMappings {
		if strings.TrimSpace(mapping.From) == "" || strings.TrimSpace(mapping.To) == "" {
			return errors.New("path mapping from and to are required")
		}
	}
	return nil
}

func normalizeConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	if cfg.Provider == "" {
		cfg.Provider = ProviderJellyfin
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.LibraryID = strings.TrimSpace(cfg.LibraryID)
	cfg.SectionID = strings.TrimSpace(cfg.SectionID)
	if !cfg.RefreshOnImport {
		// Preserve explicit false on enabled configs; default true only for empty stored configs.
	}
	for i := range cfg.PathMappings {
		cfg.PathMappings[i].From = strings.TrimSpace(cfg.PathMappings[i].From)
		cfg.PathMappings[i].To = strings.TrimSpace(cfg.PathMappings[i].To)
	}
}

func normalizePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.ToSlash(filepath.Clean(value))
	if runtime.GOOS == "windows" {
		value = strings.ToLower(value)
	}
	return strings.TrimRight(value, "/")
}

func pathHasPrefix(value, prefix string) bool {
	if value == prefix {
		return true
	}
	if prefix == "/" {
		return strings.HasPrefix(value, "/")
	}
	return strings.HasPrefix(value, prefix+"/")
}

func joinMappedPath(base, rel string) string {
	if strings.Contains(base, "\\") && !strings.Contains(base, "/") {
		return strings.TrimRight(base, `\`) + `\` + strings.ReplaceAll(rel, "/", `\`)
	}
	return strings.TrimRight(base, `/`) + "/" + strings.ReplaceAll(rel, `\`, "/")
}

func joinURL(baseURL, suffix string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimRight(baseURL, "/") + suffix
	}
	parsed.Path = path.Join(parsed.Path, suffix)
	return parsed.String()
}
