//go:build windows

package storage

import (
	"golang.org/x/sys/windows"
)

// DiskFreeBytes 返回 path 所在卷的可用字节数。
func DiskFreeBytes(path string) (int64, error) {
	dir, err := resolveDir(path)
	if err != nil {
		return 0, err
	}
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(
		windows.StringToUTF16Ptr(dir),
		&freeBytesAvailable,
		&totalBytes,
		&totalFreeBytes,
	)
	if err != nil {
		return 0, err
	}
	return int64(freeBytesAvailable), nil
}
