package storage

import (
	"os"
	"path/filepath"
)

// resolveDir 取 path 所在目录；path 本身是目录时直接返回。
// 文件尚未创建时不报错，退回父目录。
func resolveDir(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return filepath.Dir(path), nil
	}
	return path, nil
}
