// Package monitor 提供轻量指标收集与告警占位。
package monitor

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Metrics 线程安全的指标集合：counter 只增，gauge 可设置。
type Metrics struct {
	mu       sync.RWMutex
	counters map[string]*atomic.Int64
	gauges   map[string]*atomic.Int64
}

// NewMetrics 创建空的指标集合。
func NewMetrics() *Metrics {
	return &Metrics{
		counters: make(map[string]*atomic.Int64),
		gauges:   make(map[string]*atomic.Int64),
	}
}

func (m *Metrics) counter(name string) *atomic.Int64 {
	m.mu.RLock()
	v, ok := m.counters[name]
	m.mu.RUnlock()
	if ok {
		return v
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.counters[name]; ok {
		return v
	}
	v = &atomic.Int64{}
	m.counters[name] = v
	return v
}

func (m *Metrics) gauge(name string) *atomic.Int64 {
	m.mu.RLock()
	v, ok := m.gauges[name]
	m.mu.RUnlock()
	if ok {
		return v
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.gauges[name]; ok {
		return v
	}
	v = &atomic.Int64{}
	m.gauges[name] = v
	return v
}

// Incr 原子累加计数器。
func (m *Metrics) Incr(name string, delta int64) {
	m.counter(name).Add(delta)
}

// Set 设置 gauge 值。
func (m *Metrics) Set(name string, value int64) {
	m.gauge(name).Store(value)
}

// Get 读取某个指标（counter 或 gauge），不存在则返回 0。
func (m *Metrics) Get(name string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.counters[name]; ok {
		return v.Load()
	}
	if v, ok := m.gauges[name]; ok {
		return v.Load()
	}
	return 0
}

// Snapshot 返回全部指标的快照，key 有序，便于 /v1/metrics 输出。
func (m *Metrics) Snapshot() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]int64, len(m.counters)+len(m.gauges))
	keys := make([]string, 0, len(m.counters)+len(m.gauges))
	for k := range m.counters {
		keys = append(keys, k)
	}
	for k := range m.gauges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v, ok := m.counters[k]; ok {
			out[k] = v.Load()
			continue
		}
		out[k] = m.gauges[k].Load()
	}
	return out
}
