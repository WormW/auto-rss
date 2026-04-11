//go:build !windows
// +build !windows

package handler

import (
	"syscall"
)

// getDiskUsage 获取指定路径的磁盘使用情况 (Unix/Linux/macOS)
func getDiskUsage(path string) (*DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		// 如果路径不存在，尝试使用根目录
		if err := syscall.Statfs("/", &stat); err != nil {
			return nil, err
		}
		path = "/"
	}

	// 计算字节数
	blockSize := uint64(stat.Bsize)
	total := blockSize * uint64(stat.Blocks)
	free := blockSize * uint64(stat.Bavail)
	used := total - free

	return &DiskUsage{
		Path:  path,
		Total: total,
		Used:  used,
		Free:  free,
	}, nil
}
