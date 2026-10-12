// Package recovery 负责启动时的存储状态恢复与一致性检查（design §71 ~ §73）。
package recovery

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/models"
	"github.com/favos-me/soss/services/monitor"
	"github.com/favos-me/soss/services/storage"
	"github.com/favos-me/soss/utils"
)

// Orphan 阈值（design §45）。
const (
	OrphanWarnRatio = 0.20
	OrphanHighRatio = 0.40
)

// Report 启动恢复报告。
type Report struct {
	DataFile        string                 `json:"data_file"`
	CapacityBytes   int64                  `json:"capacity_bytes"`
	WriteCursor     int64                  `json:"write_cursor_bytes"`
	FreeBytes       int64                  `json:"free_bytes"`
	LiveRecordBytes int64                  `json:"live_record_bytes"`
	OrphanBytes     int64                  `json:"orphan_bytes"`
	OrphanRatio     float64                `json:"orphan_ratio"`
	Corrupted       bool                   `json:"corrupted"`
	CorruptedCount  int                    `json:"corrupted_count"`
	CorruptedSample []models.ObjectVersion `json:"corrupted_sample,omitempty"`
	ReadOnly        bool                   `json:"read_only"`
}

// Run 执行启动恢复流程：
//
//  1. 登记数据文件；
//  2. 由 MAX(data_offset + record_size) 与 storage_files.write_offset 重建 write_cursor；
//  3. 统计有效数据与 orphan 空间；
//  4. 校验元数据是否指向数据文件之外；
//  5. 低水位检查并按需扩容。
func Run(ctx context.Context, ds models.SossDataSource, store storage.StorageEngine,
	cfg *config.Config, logger *zap.Logger, metrics *monitor.Metrics, alarms *monitor.Alarms) (*Report, error) {
	now := time.Now().Unix()

	report := &Report{DataFile: store.DataFileName()}

	sf, err := ds.EnsureStorageFile(ctx, store.DataFileName(), store.Capacity(), now)
	if err != nil {
		return nil, err
	}

	// 1. 重建 write_cursor：绝不能简单使用文件大小（design §11）。
	maxEnd, err := ds.MaxRecordEnd(ctx)
	if err != nil {
		return nil, err
	}
	cursor := maxEnd
	if sf.WriteOffset > cursor {
		cursor = sf.WriteOffset
	}
	store.SetWriteCursor(cursor)

	// 2. 有效数据与 orphan 统计（design §44）。
	live, err := ds.SumRecordSize(ctx)
	if err != nil {
		return nil, err
	}
	store.SetLiveRecordBytes(live)

	// 3. 低水位检查：剩余不足则立即扩容（design §72）。
	if err := store.EnsureCapacity(cfg.Storage.LowWatermark.Int64()); err != nil {
		logger.Warn("startup capacity ensure failed", zap.Error(err))
	}
	capacity := store.Capacity()
	if capacity > sf.CapacityBytes {
		if err := ds.UpdateStorageFileCapacity(ctx, store.DataFileName(), capacity, cursor); err != nil {
			logger.Warn("update storage file capacity failed", zap.Error(err))
		}
	}

	// 4. 严重一致性检查：元数据指向数据文件之外 -> 只读安全模式（design §73）。
	corrupted, err := ds.CheckCorruption(ctx, capacity)
	if err != nil {
		return nil, err
	}
	report.Corrupted = len(corrupted) > 0
	report.CorruptedCount = len(corrupted)
	report.CorruptedSample = corrupted
	report.ReadOnly = report.Corrupted
	if report.Corrupted {
		alarms.StorageCorrupted(fmt.Sprintf("%d record(s) point outside %s (capacity %s)",
			len(corrupted), store.DataFileName(), utils.FormatSize(capacity)))
	}

	// 5. 汇总报告与指标。
	cursor = store.WriteCursor()
	orphan := cursor - live
	if orphan < 0 {
		orphan = 0
	}
	free := capacity - cursor
	if free < 0 {
		free = 0
	}
	report.CapacityBytes = capacity
	report.WriteCursor = cursor
	report.FreeBytes = free
	report.LiveRecordBytes = live
	report.OrphanBytes = orphan
	if cursor > 0 {
		report.OrphanRatio = float64(orphan) / float64(cursor)
	}

	if metrics != nil {
		metrics.Set(consts.MetricOrphanBytes, orphan)
		metrics.Set(consts.MetricLiveRecordBytes, live)
	}

	logger.Info("recovery finished",
		zap.String(consts.LogFieldNameOperation, consts.OperationStartupRecover),
		zap.String("data_file", report.DataFile),
		zap.String("capacity", utils.FormatSize(capacity)),
		zap.String("write_cursor", utils.FormatSize(cursor)),
		zap.String("free", utils.FormatSize(free)),
		zap.String("live", utils.FormatSize(live)),
		zap.String("orphan", utils.FormatSize(orphan)),
		zap.Float64("orphan_ratio", report.OrphanRatio),
		zap.Bool("read_only", report.ReadOnly),
	)

	if report.OrphanRatio >= OrphanHighRatio {
		alarms.OrphanRatio(report.OrphanRatio, orphan)
	}
	if free < cfg.Storage.LowWatermark.Int64() {
		alarms.DataCapacityLow(free, cfg.Storage.LowWatermark.Int64())
	}
	return report, nil
}
