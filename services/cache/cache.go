// Package cache 实现 HOT 全量缓存与 NORMAL LRU 缓存。
//
// 铁律（design §27）：缓存永远不是事实来源，只有在 data.bin 写入并 fsync、
// SQLite 事务提交成功之后才允许更新缓存。
package cache

import (
	"fmt"
	"strconv"
	"strings"
)

// EntryOverheadBytes 单个缓存条目的结构性开销估算（map/list/指针/字符串头等），
// 用于按字节容量做准入判断时更贴近真实内存占用。
const EntryOverheadBytes = 256

// CacheEntry 缓存条目（design §65）。
//
// Data 切片在放入缓存后视为只读，任何调用方不得修改其内容。
type CacheEntry struct {
	BucketID    int64
	ObjectID    int64
	ObjectKey   string
	Version     int64
	Size        int64
	Filename    string
	ContentType string
	ETag        string
	Checksum    string
	Data        []byte
}

// MemoryBytes 返回该条目占用的内存估算值。
func (e *CacheEntry) MemoryBytes() int64 {
	return e.Size + EntryOverheadBytes
}

// CacheKey 缓存键：bucketID/objectKey/version。
//
// 设计文档 §64 建议 bucketID/objectID/version，§24 也允许 bucketID/objectKey/version。
// 这里选择 objectKey：读路径可以先查缓存，避免每次 GET 都为了拿 objectID 去查一次 SQLite，
// 只有缓存未命中时才落库。objectKey 在对象的整个生命周期内不可变，因此同样稳定。
func CacheKey(bucketID int64, objectKey string, version int64) string {
	var b strings.Builder
	b.Grow(len(objectKey) + 48)
	b.WriteString(strconv.FormatInt(bucketID, 10))
	b.WriteByte('/')
	b.WriteString(objectKey)
	b.WriteByte('/')
	b.WriteString(strconv.FormatInt(version, 10))
	return b.String()
}

// ObjectKey 对象级索引键：bucketID/objectKey，用于对象覆盖写/删除时批量失效。
func ObjectKey(bucketID int64, objectKey string) string {
	return strconv.FormatInt(bucketID, 10) + "/" + objectKey
}

// CacheStats 缓存统计。
type CacheStats struct {
	UsedBytes int64 `json:"used_bytes"`
	MaxBytes  int64 `json:"max_bytes"`
	Objects   int64 `json:"objects"`
	Evictions int64 `json:"evictions"`
}

// Usage 返回使用率（0~1），max<=0 时返回 0。
func (s CacheStats) Usage() float64 {
	if s.MaxBytes <= 0 {
		return 0
	}
	return float64(s.UsedBytes) / float64(s.MaxBytes)
}

// String 返回可读描述。
func (s CacheStats) String() string {
	return fmt.Sprintf("objects=%d used=%d/%d", s.Objects, s.UsedBytes, s.MaxBytes)
}
