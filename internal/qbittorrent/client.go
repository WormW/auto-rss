package qbittorrent

type Client interface {
	Login(host, username, password string) error
	TestConnection(host, username, password string) error
	AddTorrent(torrentURL string, savePath string, category string) (string, error)
	AddTorrentFile(filename string, fileContent []byte, savePath string, category string) (string, error)
	GetTorrentInfo(hash string) (*TorrentInfo, error)
	GetTorrentsByCategory(category string) ([]*TorrentInfo, error)
	SetCategory(hash string, category string) error
	SetLocation(hash string, location string) error
	RenameTorrentFile(hash string, oldPath string, newPath string) error
	PauseTorrent(hash string) error
	ResumeTorrent(hash string) error
	RemoveTorrentTask(hash string) error
	DeleteTorrentWithPayload(hash string) error
	GetTorrentFiles(hash string) ([]TorrentFile, error)
	GetVersion() (string, error)
	SetProxy(proxyURL string) error
	DownloadTorrentFile(url string) ([]byte, error)
}

type TorrentInfo struct {
	Hash       string
	Name       string
	Progress   float64
	State      string
	SavePath   string
	Category   string
	Size       int64
	Downloaded int64
	Uploaded   int64
}

type TorrentFile struct {
	Name     string
	Size     int64
	Progress float64
}
