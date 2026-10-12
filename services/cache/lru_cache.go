package cache

import (
	"container/list"
	"strconv"
	"sync"
)

// LRUCache NORMAL 桶缓存：按字节容量为主、对象个数为辅的双上限 LRU（design §68）。
//
// 同时维护对象级索引 byObject（bucketID/objectID -> []cacheKey），
// 以便对象被覆盖写或删除时批量失效（design §27）。
type LRUCache struct {
	mu         sync.Mutex
	maxBytes   int64
	maxObjects int64
	usedBytes  int64
	evictions  int64
	items      map[string]*list.Element
	byObject   map[string]map[string]struct{}
	order      *list.List
}

// NewLRUCache 创建 LRU 缓存。
func NewLRUCache(maxBytes, maxObjects int64) *LRUCache {
	if maxObjects <= 0 {
		maxObjects = 1 << 40
	}
	return &LRUCache{
		maxBytes:   maxBytes,
		maxObjects: maxObjects,
		items:      make(map[string]*list.Element),
		byObject:   make(map[string]map[string]struct{}),
		order:      list.New(),
	}
}

// Get 读取缓存条目并更新 LRU 位置。
func (c *LRUCache) Get(key string) (CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.items[key]
	if !ok {
		return CacheEntry{}, false
	}
	c.order.MoveToFront(elem)
	return elem.Value.(*CacheEntry).shallowCopy(), true
}

// Put 写入缓存条目并按需淘汰。
func (c *LRUCache) Put(entry *CacheEntry) {
	if entry == nil {
		return
	}
	key := CacheKey(entry.BucketID, entry.ObjectKey, entry.Version)
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		old := elem.Value.(*CacheEntry)
		c.usedBytes -= old.MemoryBytes()
		c.usedBytes += entry.MemoryBytes()
		elem.Value = entry
		c.order.MoveToFront(elem)
		c.evictLocked()
		return
	}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
	c.usedBytes += entry.MemoryBytes()
	c.addIndexLocked(ObjectKey(entry.BucketID, entry.ObjectKey), key)
	c.evictLocked()
}

// InvalidateObject 失效某个对象的全部版本缓存条目（覆盖写 / 删除时使用）。
func (c *LRUCache) InvalidateObject(bucketID int64, objectKey string) {
	objKey := ObjectKey(bucketID, objectKey)
	c.mu.Lock()
	defer c.mu.Unlock()
	keys, ok := c.byObject[objKey]
	if !ok {
		return
	}
	for key := range keys {
		if elem, exists := c.items[key]; exists {
			c.usedBytes -= elem.Value.(*CacheEntry).MemoryBytes()
			c.order.Remove(elem)
			delete(c.items, key)
			c.evictions++
		}
	}
	delete(c.byObject, objKey)
}

// DeleteBucket 失效整个桶的缓存条目。
func (c *LRUCache) DeleteBucket(bucketID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := strconv.FormatInt(bucketID, 10) + "/"
	for objKey := range c.byObject {
		if len(objKey) <= len(prefix) || objKey[:len(prefix)] != prefix {
			continue
		}
		for key := range c.byObject[objKey] {
			if elem, exists := c.items[key]; exists {
				c.usedBytes -= elem.Value.(*CacheEntry).MemoryBytes()
				c.order.Remove(elem)
				delete(c.items, key)
				c.evictions++
			}
		}
		delete(c.byObject, objKey)
	}
}

// Stats 返回 LRU 统计。
func (c *LRUCache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return CacheStats{
		UsedBytes: c.usedBytes,
		MaxBytes:  c.maxBytes,
		Objects:   int64(len(c.items)),
		Evictions: c.evictions,
	}
}

// evictLocked 在超过任一上限时从尾部淘汰。
func (c *LRUCache) evictLocked() {
	for (c.maxBytes > 0 && c.usedBytes > c.maxBytes) || int64(len(c.items)) > c.maxObjects {
		elem := c.order.Back()
		if elem == nil {
			return
		}
		entry := elem.Value.(*CacheEntry)
		key := CacheKey(entry.BucketID, entry.ObjectKey, entry.Version)
		c.order.Remove(elem)
		delete(c.items, key)
		c.removeIndexLocked(ObjectKey(entry.BucketID, entry.ObjectKey), key)
		c.usedBytes -= entry.MemoryBytes()
		c.evictions++
	}
}

func (c *LRUCache) addIndexLocked(objKey, cacheKey string) {
	set, ok := c.byObject[objKey]
	if !ok {
		set = make(map[string]struct{}, 1)
		c.byObject[objKey] = set
	}
	set[cacheKey] = struct{}{}
}

func (c *LRUCache) removeIndexLocked(objKey, cacheKey string) {
	set, ok := c.byObject[objKey]
	if !ok {
		return
	}
	delete(set, cacheKey)
	if len(set) == 0 {
		delete(c.byObject, objKey)
	}
}

func (e *CacheEntry) shallowCopy() CacheEntry {
	return *e
}
