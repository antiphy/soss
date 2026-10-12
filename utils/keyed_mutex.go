package utils

import (
	"hash/fnv"
	"sync"
)

// KeyedMutex 基于分片的对象级互斥锁。
//
// 用途：同一个 object key 的并发写入需要串行化（版本号生成依赖 latest_version），
// 但不同 key 之间不应互相阻塞。分片数量固定，避免为每个 key 永久保留一个 Mutex
// 导致内存无限增长。
type KeyedMutex struct {
	shards []sync.Mutex
}

// NewKeyedMutex 创建分片锁，shards 建议取 2 的幂。
func NewKeyedMutex(shards int) *KeyedMutex {
	if shards <= 0 {
		shards = 512
	}
	return &KeyedMutex{shards: make([]sync.Mutex, shards)}
}

func (m *KeyedMutex) index(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() & uint32(len(m.shards)-1))
}

// Lock 按 key 加锁。
func (m *KeyedMutex) Lock(key string) {
	m.shards[m.index(key)].Lock()
}

// Unlock 按 key 解锁。
func (m *KeyedMutex) Unlock(key string) {
	m.shards[m.index(key)].Unlock()
}
