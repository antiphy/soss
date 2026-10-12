package storage

import (
	"fmt"
	"os"
	"sync"

	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/services/monitor"
	"github.com/favos-me/soss/utils"
)

// Allocator 负责 write_cursor 维护与数据文件扩容（design §9 / §46 / §47）。
//
// 锁只保护"分配"这一瞬间：拿到 offset 后立即释放，真正的 pwrite 由调用方并发执行，
// 因为每个请求写入的是互不重叠的磁盘区域（design §10）。
type Allocator struct {
	path    string
	cfg     config.StorageConfig
	logger  *zap.Logger
	metrics *monitor.Metrics
	alarms  *monitor.Alarms
	file    *os.File

	mu          sync.Mutex
	capacity    int64
	writeCursor int64
	expandCount int64
}

// NewAllocator 创建分配器，并保证数据文件达到初始容量。
func NewAllocator(file *os.File, path string, cfg config.StorageConfig,
	logger *zap.Logger, metrics *monitor.Metrics, alarms *monitor.Alarms) (*Allocator, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Wrap(errors.CodeStorageError, err)
	}
	a := &Allocator{
		path:     path,
		cfg:      cfg,
		logger:   logger,
		metrics:  metrics,
		alarms:   alarms,
		file:     file,
		capacity: info.Size(),
	}
	if err := a.ensureInitialCapacity(); err != nil {
		return nil, err
	}
	a.publishMetrics()
	return a, nil
}

// ensureInitialCapacity 文件不存在或小于 initial_size 时补足到 initial_size（design §46.1）。
func (a *Allocator) ensureInitialCapacity() error {
	initial := a.cfg.InitialSize.Int64()
	if a.capacity >= initial {
		return nil
	}
	if err := a.file.Truncate(initial); err != nil {
		return errors.Wrap(errors.CodeStorageError, err)
	}
	a.capacity = initial
	a.logger.Info("data file initialized",
		zap.String(consts.LogFieldNameOperation, consts.OperationExpand),
		zap.String("path", a.path),
		zap.String("size", utils.FormatSize(initial)),
	)
	return nil
}

// Allocate 分配 size 字节的连续空间并返回 record 起始偏移。
func (a *Allocator) Allocate(size int64) (int64, error) {
	if size <= 0 {
		return 0, errors.NewErrorWithMsg(errors.CodeInvalidArgument, "allocate size must be positive")
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// 写路径同步扩容兜底：后台扩容存在时间窗口，不能完全依赖它（design §49）。
	if err := a.ensureCapacityLocked(size); err != nil {
		return 0, err
	}
	offset := a.writeCursor
	a.writeCursor += size
	a.publishMetrics()
	return offset, nil
}

// EnsureCapacity 确保剩余可用空间不少于 required。
func (a *Allocator) EnsureCapacity(required int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ensureCapacityLocked(required)
}

// EnsureLowWatermark 后台扩容任务入口：剩余空间低于低水位时按步长扩容（design §47）。
func (a *Allocator) EnsureLowWatermark() (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	watermark := a.cfg.LowWatermark.Int64()
	free := a.capacity - a.writeCursor
	if free >= watermark {
		return free, nil
	}
	if err := a.ensureCapacityLocked(watermark - free); err != nil {
		return free, err
	}
	return a.capacity - a.writeCursor, nil
}

func (a *Allocator) ensureCapacityLocked(required int64) error {
	if required <= 0 {
		return nil
	}
	// 大对象场景：一次扩容 1GiB 可能不够，循环扩容直到满足需求（design §48）。
	for a.capacity-a.writeCursor < required {
		if err := a.expandOnceLocked(); err != nil {
			return err
		}
	}
	return nil
}

// expandOnceLocked 执行一次 +expand_step 扩容，扩容前检查物理磁盘剩余空间（design §50）。
func (a *Allocator) expandOnceLocked() error {
	step := a.cfg.ExpandStep.Int64()
	margin := a.cfg.DiskSafetyMargin.Int64()
	freeDisk, err := DiskFreeBytes(a.path)
	if err != nil {
		// 拿不到磁盘信息时不阻塞写入，仅记录，交由 Truncate 的实际结果判定。
		a.logger.Warn("probe disk free space failed", zap.Error(err))
	} else if freeDisk < step+margin {
		a.alarms.DiskSpaceInsufficient(step, freeDisk)
		return errors.NewErrorWithMsg(errors.CodeDiskFull,
			fmt.Sprintf("disk free %s < expand step %s + margin %s",
				utils.FormatSize(freeDisk), utils.FormatSize(step), utils.FormatSize(margin)))
	}
	target := a.capacity + step
	if err := a.file.Truncate(target); err != nil {
		return errors.Wrap(errors.CodeStorageError, err)
	}
	a.capacity = target
	a.expandCount++
	if a.metrics != nil {
		a.metrics.Incr(consts.MetricExpandTotal, 1)
	}
	a.logger.Info("data file expanded",
		zap.String(consts.LogFieldNameOperation, consts.OperationExpand),
		zap.String("path", a.path),
		zap.String("new_capacity", utils.FormatSize(target)),
		zap.String("write_cursor", utils.FormatSize(a.writeCursor)),
	)
	a.publishMetrics()
	return nil
}

// SetWriteCursor 由 recovery 在启动时回填（design §11）。
func (a *Allocator) SetWriteCursor(v int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v < 0 {
		v = 0
	}
	a.writeCursor = v
	a.publishMetrics()
}

// WriteCursor 返回当前写游标。
func (a *Allocator) WriteCursor() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.writeCursor
}

// Capacity 返回数据文件当前容量。
func (a *Allocator) Capacity() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.capacity
}

// ExpandCount 返回累计扩容次数。
func (a *Allocator) ExpandCount() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.expandCount
}

// FreeBytes 返回剩余可用空间。
func (a *Allocator) FreeBytes() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	free := a.capacity - a.writeCursor
	if free < 0 {
		return 0
	}
	return free
}

func (a *Allocator) publishMetrics() {
	if a.metrics == nil {
		return
	}
	a.metrics.Set(consts.MetricDataFileSize, a.capacity)
	a.metrics.Set(consts.MetricDataWriteCursor, a.writeCursor)
	free := a.capacity - a.writeCursor
	if free < 0 {
		free = 0
	}
	a.metrics.Set(consts.MetricDataFreeCapacity, free)
}
