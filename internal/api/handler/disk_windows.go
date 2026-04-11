//go:build windows
// +build windows

package handler

import (
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// getDiskUsage 获取指定路径的磁盘使用情况 (Windows)
func getDiskUsage(path string) (*DiskUsage, error) {
	// 尝试获取路径的磁盘信息
	lpDirectoryName, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		// 如果失败，尝试使用根目录
		lpDirectoryName, _ = syscall.UTF16PtrFromString("C:")
		path = "C:"
	}

	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes int64

	ret, _, _ := procGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(lpDirectoryName)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)

	if ret == 0 {
		// 如果失败，尝试 C 盘
		lpDirectoryName, _ = syscall.UTF16PtrFromString("C:")
		ret, _, _ = procGetDiskFreeSpaceEx.Call(
			uintptr(unsafe.Pointer(lpDirectoryName)),
			uintptr(unsafe.Pointer(&freeBytesAvailable)),
			uintptr(unsafe.Pointer(&totalNumberOfBytes)),
			uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
		)
		if ret == 0 {
			return nil, syscall.GetLastError()
		}
		path = "C:"
	}

	used := uint64(totalNumberOfBytes - freeBytesAvailable)

	return &DiskUsage{
		Path:  path,
		Total: uint64(totalNumberOfBytes),
		Used:  used,
		Free:  uint64(freeBytesAvailable),
	}, nil
}
