// Package utils 存放纯函数小工具。
package utils

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSize 解析容量字符串，支持二进制与十进制单位：
// "1GiB" / "500MiB" / "1024" / "1GB" / "512MB"。不区分大小写。
func ParseSize(s string) (int64, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, fmt.Errorf("empty size")
	}
	raw = strings.ReplaceAll(raw, " ", "")
	i := 0
	for i < len(raw) && (raw[i] >= '0' && raw[i] <= '9' || raw[i] == '.' || raw[i] == '-' || raw[i] == '+') {
		i++
	}
	numPart := raw[:i]
	unit := strings.ToLower(raw[i:])
	if numPart == "" {
		return 0, fmt.Errorf("invalid size: %q", s)
	}
	val, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	var mul float64
	switch unit {
	case "", "b":
		mul = 1
	case "k", "kb":
		mul = 1000
	case "kib":
		mul = 1024
	case "m", "mb":
		mul = 1000 * 1000
	case "mib":
		mul = 1024 * 1024
	case "g", "gb":
		mul = 1000 * 1000 * 1000
	case "gib":
		mul = 1024 * 1024 * 1024
	case "t", "tb":
		mul = 1000 * 1000 * 1000 * 1000
	case "tib":
		mul = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size unit: %q", s)
	}
	if val < 0 {
		return 0, fmt.Errorf("negative size: %q", s)
	}
	return int64(val * mul), nil
}

// FormatSize 把字节数格式化为便于阅读的字符串。
func FormatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f%ciB", float64(n)/float64(div), "KMGTP"[exp])
}
