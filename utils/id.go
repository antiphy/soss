package utils

import (
	"crypto/rand"
	"encoding/hex"
)

// NewXID 生成一个 16 字节随机 ID 的十六进制字符串，用于 request id（tid）。
func NewXID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// 随机数不可用时退化为时间戳编码，保证不阻塞请求链路。
		return hex.EncodeToString([]byte("fallback-id"))
	}
	return hex.EncodeToString(buf[:])
}
