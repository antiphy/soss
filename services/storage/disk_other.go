//go:build !windows

package storage

import (
	"syscall"
)

// DiskFreeBytes 返回 path 所在文件系统的可用字节数（非特权用户可用空间）。
func DiskFreeBytes(path string) (int64, error) {
	dir, err := resolveDir(path)
	if err != nil {
		return 0, err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
