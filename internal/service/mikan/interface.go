package mikan

// Service 定义Mikan服务的接口
type Service interface {
	Search(keyword string) (*SearchResult, error)
	GetFansubGroups(animeURL string) ([]*FansubGroup, error)
	SetProxy(proxy string) error
}

// 确保 MikanService 实现了 Service 接口
var _ Service = (*MikanService)(nil)
