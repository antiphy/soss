package monitor

import (
	"go.uber.org/zap"

	"github.com/favos-me/soss/consts"
)

// Alarms 告警出口。第一版只落日志并计数，外部通知渠道留 TODO（design §89）。
type Alarms struct {
	logger  *zap.Logger
	metrics *Metrics
}

// NewAlarms 创建告警出口。
func NewAlarms(logger *zap.Logger, metrics *Metrics) *Alarms {
	return &Alarms{logger: logger, metrics: metrics}
}

func (a *Alarms) fire(kind string, fields ...zap.Field) {
	if a.metrics != nil {
		a.metrics.Incr(consts.MetricAlarmTotal, 1)
	}
	fs := append([]zap.Field{zap.String("alarm", kind)}, fields...)
	a.logger.Error("ALARM", fs...)
}

// DiskSpaceInsufficient 物理磁盘空间不足（design §50）。
func (a *Alarms) DiskSpaceInsufficient(needBytes, freeBytes int64) {
	a.fire("DISK_SPACE_INSUFFICIENT",
		zap.Int64("need_bytes", needBytes),
		zap.Int64("free_bytes", freeBytes),
	)
	// TODO: 接入 Slack / Email / PagerDuty / 企业微信 / 钉钉。
}

// StorageCorrupted 存储数据与元数据不一致，服务进入只读安全模式（design §73）。
func (a *Alarms) StorageCorrupted(detail string) {
	a.fire("STORAGE_CORRUPTED", zap.String("detail", detail))
	// TODO: 接入告警渠道并触发人工介入。
}

// CacheCapacityExceeded 内存缓存容量不足（design §21）。
func (a *Alarms) CacheCapacityExceeded(kind string, usedBytes, maxBytes int64) {
	a.fire("CACHE_CAPACITY_EXCEEDED",
		zap.String("cache", kind),
		zap.Int64("used_bytes", usedBytes),
		zap.Int64("max_bytes", maxBytes),
	)
}

// SQLiteError SQLite 错误（design §88）。
func (a *Alarms) SQLiteError(err error) {
	a.fire("SQLITE_ERROR", zap.Error(err))
}

// StorageReadError 存储读取错误（design §85）。
func (a *Alarms) StorageReadError(err error, offset, size int64) {
	a.fire("STORAGE_READ_ERROR",
		zap.Error(err),
		zap.Int64("offset", offset),
		zap.Int64("size", size),
	)
}

// OrphanRatio 垃圾空间占比超过阈值（design §45）。
func (a *Alarms) OrphanRatio(ratio float64, orphanBytes int64) {
	a.fire("ORPHAN_RATIO_HIGH",
		zap.Float64("orphan_ratio", ratio),
		zap.Int64("orphan_bytes", orphanBytes),
	)
}

// DataCapacityLow 数据文件剩余容量低于低水位。
func (a *Alarms) DataCapacityLow(freeBytes, watermark int64) {
	a.fire("DATA_CAPACITY_LOW",
		zap.Int64("free_bytes", freeBytes),
		zap.Int64("low_watermark", watermark),
	)
}
