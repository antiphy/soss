package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"hash/crc32"
)

var castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

// NewCRC32C 返回一个 CRC32C（Castagnoli）哈希器，用于 record header 校验。
func NewCRC32C() hash.Hash32 {
	return crc32.New(castagnoliTable)
}

// CRC32CChecksum 一次性计算 CRC32C。
func CRC32CChecksum(p []byte) uint32 {
	return crc32.Checksum(p, castagnoliTable)
}

// ETag 生成对象 ETag（design §55）：MD5 十六进制字符串。
func ETag(p []byte) string {
	sum := md5.Sum(p)
	return hex.EncodeToString(sum[:])
}

// SHA256Hex 生成对象内容的 SHA-256 十六进制字符串。
func SHA256Hex(p []byte) string {
	sum := sha256.Sum256(p)
	return hex.EncodeToString(sum[:])
}
