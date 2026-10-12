// Package storage 实现 data.bin 存储引擎：append-only 写入、偏移分配、容量管理与记录格式。
package storage

// StorageStat 存储引擎运行状态，用于监控与 /v1/metrics。
type StorageStat struct {
	FileName        string  `json:"file_name"`
	CapacityBytes   int64   `json:"capacity_bytes"`
	WriteCursor     int64   `json:"write_cursor_bytes"`
	FreeBytes       int64   `json:"free_bytes"`
	DiskFreeBytes   int64   `json:"disk_free_bytes"`
	LiveRecordBytes int64   `json:"live_record_bytes"`
	OrphanBytes     int64   `json:"orphan_bytes"`
	OrphanRatio     float64 `json:"orphan_ratio"`
	ExpandCount     int64   `json:"expand_count"`
}

// StorageEngine 存储引擎抽象（design §77）。业务层不直接操作 os.File。
type StorageEngine interface {
	// Allocate 分配一块 record_size 大小的连续空间，返回 record 起始偏移。
	// 锁只保护"分配"这一瞬间，真正的写入由调用方并发执行（design §10）。
	Allocate(size int64) (int64, error)
	// WriteRecord 在指定偏移写入 header + payload（pwrite 语义）。
	WriteRecord(offset int64, version int64, payload []byte) error
	// ReadRecord 读取指定 record，返回 payload；verify=true 时校验 CRC32C。
	ReadRecord(offset, recordSize int64, verify bool) ([]byte, error)
	// PatchRecordVersion 仅在版本号被事务校正后回填 header 的版本字段。
	PatchRecordVersion(offset int64, version int64) error
	// Sync 刷盘（fsync/fdatasync）。
	Sync() error
	// EnsureCapacity 确保剩余空间至少为 required。
	EnsureCapacity(required int64) error
	// EnsureLowWatermark 后台扩容入口：剩余空间低于低水位时按步长扩容，返回扩容后的剩余空间。
	EnsureLowWatermark() (int64, error)
	// Stat 返回运行状态。
	Stat() StorageStat
	// SetWriteCursor 启动时由 recovery 回填 write_cursor。
	SetWriteCursor(v int64)
	// WriteCursor 返回当前写游标。
	WriteCursor() int64
	// Capacity 返回数据文件当前容量。
	Capacity() int64
	// DataFileName 返回数据文件名（写入元数据 data_file 字段）。
	DataFileName() string
	// SetLiveRecordBytes 设置有效数据字节数，用于 orphan 统计。
	SetLiveRecordBytes(v int64)
	// Close 关闭数据文件。
	Close() error
}
