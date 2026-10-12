package cache

import (
	"sync"

	"github.com/favos-me/soss/errors"
)

// HotCache HOT 桶缓存：尽量让桶内全部当前对象常驻内存（design §20）。
//
// 数据结构：map[bucketID]map[objectKey]*CacheEntry，读写分离锁。
// 必须受内存上限约束，避免把超大 bucket 标记成 HOT 导致 OOM（design §21）。
type HotCache struct {
	mu        sync.RWMutex
	maxBytes  int64
	usedBytes int64
	objects   int64
	data      map[int64]map[string]*CacheEntry
}

// NewHotCache 创建 HOT 缓存，maxBytes <= 0 表示不限制（不建议）。
func NewHotCache(maxBytes int64) *HotCache {
	return &HotCache{
		maxBytes: maxBytes,
		data:     make(map[int64]map[string]*CacheEntry),
	}
}

// Get 读取缓存条目，返回值拷贝（Data 仍共享切片，只读使用）。
func (c *HotCache) Get(bucketID int64, objectKey string) (CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	bucket, ok := c.data[bucketID]
	if !ok {
		return CacheEntry{}, false
	}
	entry, ok := bucket[objectKey]
	if !ok {
		return CacheEntry{}, false
	}
	return *entry, true
}

// CanAdmit 判断能否再放入 size 大小的对象，超过上限返回 false。
func (c *HotCache) CanAdmit(size int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.maxBytes <= 0 {
		return true
	}
	return c.usedBytes+size+EntryOverheadBytes <= c.maxBytes
}

// Put 写入/替换一个缓存条目，容量不足返回 CodeCacheCapacityExceeded。
func (c *HotCache) Put(entry *CacheEntry) error {
	if entry == nil {
		return errors.NewError(errors.CodeInvalidArgument)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	add := entry.MemoryBytes()
	replaced := int64(0)
	bucket, ok := c.data[entry.BucketID]
	if !ok {
		bucket = make(map[string]*CacheEntry)
		c.data[entry.BucketID] = bucket
	} else if old, exists := bucket[entry.ObjectKey]; exists {
		replaced = old.MemoryBytes()
	}
	if c.maxBytes > 0 && c.usedBytes-replaced+add > c.maxBytes {
		return errors.NewErrorWithMsg(errors.CodeCacheCapacityExceeded, "hot cache capacity exceeded")
	}
	bucket[entry.ObjectKey] = entry
	c.usedBytes += add - replaced
	if !ok || replaced == 0 {
		c.objects++
	}
	return nil
}

// Delete 删除某个对象的缓存条目。
func (c *HotCache) Delete(bucketID int64, objectKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bucket, ok := c.data[bucketID]
	if !ok {
		return
	}
	if old, exists := bucket[objectKey]; exists {
		c.usedBytes -= old.MemoryBytes()
		c.objects--
		delete(bucket, objectKey)
	}
}

// DeleteBucket 删除整个桶的缓存（桶被删除或 HOT -> NORMAL 时使用，design §32 / §66）。
func (c *HotCache) DeleteBucket(bucketID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bucket, ok := c.data[bucketID]
	if !ok {
		return
	}
	for _, entry := range bucket {
		c.usedBytes -= entry.MemoryBytes()
		c.objects--
	}
	delete(c.data, bucketID)
}

// HasBucket 判断桶是否已在 HOT 缓存中。
func (c *HotCache) HasBucket(bucketID int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.data[bucketID]
	return ok
}

// Stats 返回 HOT 缓存统计。
func (c *HotCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CacheStats{
		UsedBytes: c.usedBytes,
		MaxBytes:  c.maxBytes,
		Objects:   c.objects,
	}
}
