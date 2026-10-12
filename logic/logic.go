// Package logic 业务流程编排层：只依赖 models 接口与 services 能力，不含 HTTP/gRPC 类型。
package logic

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/consts/types"
	"github.com/favos-me/soss/models"
	"github.com/favos-me/soss/services/cache"
	"github.com/favos-me/soss/services/monitor"
	"github.com/favos-me/soss/services/recovery"
	"github.com/favos-me/soss/services/storage"
	"github.com/favos-me/soss/utils"
)

// Logic 全局业务入口，由 main 在启动阶段注入依赖。
var Logic *Manager

// Manager 业务编排器，持有全部领域依赖。
type Manager struct {
	cfg     *config.Config
	logger  *zap.Logger
	ds      models.SossDataSource
	store   storage.StorageEngine
	hot     *cache.HotCache
	lru     *cache.LRUCache
	metrics *monitor.Metrics
	alarms  *monitor.Alarms

	// sfGroup 防止缓存击穿：同一对象的并发加载只回源一次（design §26）。
	sfGroup *singleflight.Group
	// latestVer 对象最新版本号的轻量索引，让 NORMAL 读路径先查 LRU 再决定是否落库。
	latestVer sync.Map
	// keyMu 对象级分片锁，串行化同一 object key 的写入（版本号生成依赖 latest_version）。
	keyMu *utils.KeyedMutex

	bucketCache   map[string]models.Bucket
	bucketCacheMu sync.RWMutex

	// registeredCapacity 已登记到 storage_files 的容量，避免重复写库。
	registeredCapacity atomic.Int64
	// liveBytes 有效数据字节数（增量维护，用于 orphan 统计）。
	liveBytes atomic.Int64
	// readOnly 严重数据损坏时进入只读安全模式（design §73）。
	readOnly atomic.Bool

	capacityCh chan struct{}
	stopCh     chan struct{}

	// taskMu/stopped 保证 Stop 之后不会再向 WaitGroup 追加任务。
	taskMu  sync.Mutex
	stopped bool
	wg      sync.WaitGroup
}

// Init 构造全局业务入口。
func Init(cfg *config.Config, logger *zap.Logger, ds models.SossDataSource,
	store storage.StorageEngine, metrics *monitor.Metrics, alarms *monitor.Alarms) *Manager {
	m := &Manager{
		cfg:         cfg,
		logger:      logger,
		ds:          ds,
		store:       store,
		hot:         cache.NewHotCache(cfg.Cache.HotMaxBytes.Int64()),
		lru:         cache.NewLRUCache(cfg.Cache.LRUMaxBytes.Int64(), cfg.Cache.LRUMaxObjects),
		metrics:     metrics,
		alarms:      alarms,
		sfGroup:     &singleflight.Group{},
		keyMu:       utils.NewKeyedMutex(1024),
		bucketCache: make(map[string]models.Bucket),
		capacityCh:  make(chan struct{}, consts.CapacitySignalBuffer),
		stopCh:      make(chan struct{}),
	}
	Logic = m
	return m
}

// Start 启动后台任务：容量巡检 worker 与 HOT 桶 warm-up。
func (m *Manager) Start(report *recovery.Report) {
	if report != nil {
		m.registeredCapacity.Store(report.CapacityBytes)
		m.liveBytes.Store(report.LiveRecordBytes)
		m.readOnly.Store(report.ReadOnly)
	}
	m.goAsync(m.capacityWorker)

	// HOT 桶后台 warm-up，不阻塞服务启动（design §22）。
	ctx := context.Background()
	buckets, err := m.ds.ListBuckets(ctx)
	if err != nil {
		m.logger.Error("list buckets for warm-up failed", zap.Error(err))
		return
	}
	for _, b := range buckets {
		if b.Class != types.BucketClassHot {
			continue
		}
		bucket := b
		m.goAsync(func() { m.warmUpBucket(bucket) })
	}
}

// Stop 停止后台任务并等待结束。
func (m *Manager) Stop() {
	m.taskMu.Lock()
	if !m.stopped {
		m.stopped = true
		close(m.stopCh)
	}
	m.taskMu.Unlock()
	m.wg.Wait()
}

// goAsync 启动一个可被 Stop 等待的后台任务；Stop 之后调用会被忽略。
func (m *Manager) goAsync(fn func()) {
	m.taskMu.Lock()
	defer m.taskMu.Unlock()
	if m.stopped {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		fn()
	}()
}

// Cfg 暴露配置，供传输层读取只读参数（如单次上传大小上限）。
func (m *Manager) Cfg() *config.Config { return m.cfg }

// SetReadOnly 切换只读安全模式。
func (m *Manager) SetReadOnly(v bool) { m.readOnly.Store(v) }

// ReadOnly 返回是否处于只读模式。
func (m *Manager) ReadOnly() bool { return m.readOnly.Load() }

// ---------- bucket 元数据缓存 ----------

func (m *Manager) getBucket(ctx context.Context, name string) (models.Bucket, error) {
	m.bucketCacheMu.RLock()
	b, ok := m.bucketCache[name]
	m.bucketCacheMu.RUnlock()
	if ok {
		return b, nil
	}
	b, err := m.ds.GetBucketByName(ctx, name)
	if err != nil {
		return models.Bucket{}, err
	}
	m.bucketCacheMu.Lock()
	m.bucketCache[name] = b
	m.bucketCacheMu.Unlock()
	return b, nil
}

func (m *Manager) invalidateBucket(name string) {
	m.bucketCacheMu.Lock()
	delete(m.bucketCache, name)
	m.bucketCacheMu.Unlock()
}

func (m *Manager) invalidateBucketByID(id int64) {
	m.bucketCacheMu.Lock()
	for name, b := range m.bucketCache {
		if b.ID == id {
			delete(m.bucketCache, name)
		}
	}
	m.bucketCacheMu.Unlock()
}

// ---------- 后台任务 ----------

// capacityWorker 容量巡检循环。注意：WaitGroup 的 Done 由 goAsync 统一负责，
// 此处不可再调用 m.wg.Done()，否则会导致计数器变负而 panic。
func (m *Manager) capacityWorker() {
	ticker := time.NewTicker(consts.CapacityCheckIntervalSec * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-m.capacityCh:
			m.ensureCapacity()
		case <-ticker.C:
			m.ensureCapacity()
		}
	}
}

// ensureCapacity 后台扩容 + 指标刷新（design §47 / §49）。
func (m *Manager) ensureCapacity() {
	free, err := m.store.EnsureLowWatermark()
	if err != nil {
		m.logger.Error("background capacity ensure failed",
			zap.String(consts.LogFieldNameOperation, consts.OperationCapacityCheck),
			zap.Error(err))
		return
	}
	capacity := m.store.Capacity()
	if capacity > m.registeredCapacity.Load() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := m.ds.UpdateStorageFileCapacity(ctx, m.store.DataFileName(), capacity, m.store.WriteCursor()); err != nil {
			m.logger.Warn("update storage_files capacity failed", zap.Error(err))
		}
		cancel()
		m.registeredCapacity.Store(capacity)
	}

	stat := m.store.Stat()
	m.metrics.Set(consts.MetricOrphanBytes, stat.OrphanBytes)
	m.metrics.Set(consts.MetricLiveRecordBytes, stat.LiveRecordBytes)
	m.metrics.Set(consts.MetricDiskFreeBytes, stat.DiskFreeBytes)
	if stat.FreeBytes < m.cfg.Storage.LowWatermark.Int64() {
		m.alarms.DataCapacityLow(stat.FreeBytes, m.cfg.Storage.LowWatermark.Int64())
	}
	if stat.OrphanRatio >= recovery.OrphanHighRatio {
		m.alarms.OrphanRatio(stat.OrphanRatio, stat.OrphanBytes)
	}
	m.logger.Debug("capacity checked",
		zap.String(consts.LogFieldNameOperation, consts.OperationCapacityCheck),
		zap.String("free", utils.FormatSize(free)),
		zap.String("capacity", utils.FormatSize(capacity)),
	)
}

// warmUpBucket 后台加载 HOT 桶的全部当前版本对象（design §22）。
func (m *Manager) warmUpBucket(b models.Bucket) {
	ctx := context.Background()
	start := time.Now()
	views, err := m.ds.ListLatestVersions(ctx, b.ID, b.Name)
	if err != nil {
		m.logger.Error("list hot bucket objects failed",
			zap.String(consts.LogFieldNameBucket, b.Name), zap.Error(err))
		return
	}
	loaded, skipped := 0, 0
	for _, v := range views {
		select {
		case <-m.stopCh:
			return
		default:
		}
		if entry, ok := m.hot.Get(b.ID, v.ObjectKey); ok && entry.Version >= v.Version {
			skipped++
			continue
		}
		data, err := m.store.ReadRecord(v.DataOffset, v.RecordSize, false)
		if err != nil {
			m.logger.Error("hot warm-up read failed",
				zap.String(consts.LogFieldNameBucket, b.Name),
				zap.String(consts.LogFieldNameObjectKey, v.ObjectKey),
				zap.Error(err))
			continue
		}
		entry := &cache.CacheEntry{
			BucketID:    b.ID,
			ObjectID:    v.ObjectID,
			ObjectKey:   v.ObjectKey,
			Version:     v.Version,
			Size:        v.DataSize,
			Filename:    v.Filename,
			ContentType: v.ContentType,
			ETag:        v.ETag,
			Checksum:    v.Checksum,
			Data:        data,
		}
		if err := m.hot.Put(entry); err != nil {
			stats := m.hot.Stats()
			m.alarms.CacheCapacityExceeded("hot", stats.UsedBytes, stats.MaxBytes)
			m.logger.Error("hot warm-up aborted: capacity exceeded",
				zap.String(consts.LogFieldNameBucket, b.Name),
				zap.Error(err))
			return
		}
		loaded++
	}
	m.logger.Info("hot bucket warm-up finished",
		zap.String(consts.LogFieldNameOperation, consts.OperationHotWarmUp),
		zap.String(consts.LogFieldNameBucket, b.Name),
		zap.Int("loaded", loaded),
		zap.Int("skipped", skipped),
		zap.Int64(consts.LogFieldNameLatency, time.Since(start).Milliseconds()),
	)
}

// ---------- 通用能力 ----------

// triggerCapacityCheck 异步触发一次容量检查，通道满时丢弃（已有检查在途）。
func (m *Manager) triggerCapacityCheck() {
	select {
	case m.capacityCh <- struct{}{}:
	default:
	}
}

// Stats 汇总运行态指标，供 /v1/metrics 使用。
func (m *Manager) Stats() map[string]any {
	stat := m.store.Stat()
	hotStats := m.hot.Stats()
	lruStats := m.lru.Stats()
	snapshot := m.metrics.Snapshot()
	return map[string]any{
		"storage":   stat,
		"hot":       hotStats,
		"lru":       lruStats,
		"counters":  snapshot,
		"read_only": m.ReadOnly(),
	}
}
