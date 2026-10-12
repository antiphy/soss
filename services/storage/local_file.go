package storage

import (
	"os"
	"path/filepath"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/services/monitor"
)

// LocalFileStorage 基于单个本地数据文件的存储引擎实现（design §77）。
//
// v1 只有一个 data.bin，但通过 StorageEngine 接口与 storage_files.file_id 抽象，
// 未来可平滑升级为多 segment 文件与在线 Compact。
type LocalFileStorage struct {
	*Allocator

	cfg     config.StorageConfig
	logger  *zap.Logger
	metrics *monitor.Metrics
	alarms  *monitor.Alarms

	path      string
	fileName  string
	file      *os.File
	liveBytes atomic.Int64
}

// Open 打开（必要时创建）数据文件并完成初始容量校验。
func Open(root string, cfg config.StorageConfig,
	logger *zap.Logger, metrics *monitor.Metrics, alarms *monitor.Alarms) (*LocalFileStorage, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, errors.Wrap(errors.CodeStorageError, err)
	}
	name := cfg.DataFile
	if name == "" {
		name = consts.DataFileName
	}
	path := filepath.Join(root, name)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, errors.Wrap(errors.CodeStorageError, err)
	}
	allocator, err := NewAllocator(file, path, cfg, logger, metrics, alarms)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &LocalFileStorage{
		Allocator: allocator,
		cfg:       cfg,
		logger:    logger,
		metrics:   metrics,
		alarms:    alarms,
		path:      path,
		fileName:  name,
		file:      file,
	}, nil
}

// WriteRecord 采用位置写入（pwrite 语义），多个对象可在不同 offset 并发写（design §53）。
func (s *LocalFileStorage) WriteRecord(offset int64, version int64, payload []byte) error {
	header := NewHeader(version, int64(len(payload)), 0)
	header.CRC32C = Checksum(payload)
	buf := EncodeHeader(header)
	if _, err := s.file.WriteAt(buf[:], offset); err != nil {
		return errors.Wrap(errors.CodeStorageError, err)
	}
	if len(payload) > 0 {
		if _, err := s.file.WriteAt(payload, offset+HeaderSize); err != nil {
			return errors.Wrap(errors.CodeStorageError, err)
		}
	}
	return nil
}

// ReadRecord 采用位置读取（pread 语义），并发请求互不干扰（design §52）。
func (s *LocalFileStorage) ReadRecord(offset, recordSize int64, verify bool) ([]byte, error) {
	if recordSize < HeaderSize {
		return nil, errors.NewErrorWithMsg(errors.CodeStorageError, ErrBadHeaderSize.Error())
	}
	headerBuf := make([]byte, HeaderSize)
	if _, err := s.file.ReadAt(headerBuf, offset); err != nil {
		s.alarms.StorageReadError(err, offset, recordSize)
		return nil, errors.Wrap(errors.CodeStorageError, err)
	}
	header, err := DecodeHeader(headerBuf)
	if err != nil {
		s.alarms.StorageReadError(err, offset, recordSize)
		return nil, errors.Wrap(errors.CodeStorageError, err)
	}
	payloadSize := recordSize - HeaderSize
	if header.PayloadSize != payloadSize {
		err := ErrBadPayloadSize
		s.alarms.StorageReadError(err, offset, recordSize)
		return nil, errors.Wrap(errors.CodeStorageError, err)
	}
	payload := make([]byte, payloadSize)
	if payloadSize > 0 {
		if _, err := s.file.ReadAt(payload, offset+HeaderSize); err != nil {
			s.alarms.StorageReadError(err, offset, recordSize)
			return nil, errors.Wrap(errors.CodeStorageError, err)
		}
	}
	if verify && Checksum(payload) != header.CRC32C {
		err := ErrChecksumMismatch
		s.alarms.StorageReadError(err, offset, recordSize)
		return nil, errors.Wrap(errors.CodeStorageError, err)
	}
	if s.metrics != nil {
		s.metrics.Incr(consts.MetricGetBytesTotal, payloadSize)
	}
	return payload, nil
}

// PatchRecordVersion 回填 header 中的版本号字段。
//
// 仅在"预生成版本号被事务校正"的极少数场景使用：版本号本身以 SQLite 为准，
// header 中的版本仅用于离线扫描与恢复（design §60）。
func (s *LocalFileStorage) PatchRecordVersion(offset int64, version int64) error {
	buf := EncodeVersionField(version)
	if _, err := s.file.WriteAt(buf[:], offset+VersionFieldOffset); err != nil {
		return errors.Wrap(errors.CodeStorageError, err)
	}
	return nil
}

// Sync 刷盘。
func (s *LocalFileStorage) Sync() error {
	if !s.cfg.FsyncEnabled() {
		return nil
	}
	if err := s.file.Sync(); err != nil {
		return errors.Wrap(errors.CodeStorageError, err)
	}
	return nil
}

// Stat 返回存储引擎运行状态。
func (s *LocalFileStorage) Stat() StorageStat {
	cursor := s.WriteCursor()
	capacity := s.Capacity()
	free := capacity - cursor
	if free < 0 {
		free = 0
	}
	live := s.liveBytes.Load()
	orphan := cursor - live
	if orphan < 0 {
		orphan = 0
	}
	var ratio float64
	if cursor > 0 {
		ratio = float64(orphan) / float64(cursor)
	}
	diskFree, err := DiskFreeBytes(s.path)
	if err != nil {
		diskFree = -1
	}
	return StorageStat{
		FileName:        s.fileName,
		CapacityBytes:   capacity,
		WriteCursor:     cursor,
		FreeBytes:       free,
		DiskFreeBytes:   diskFree,
		LiveRecordBytes: live,
		OrphanBytes:     orphan,
		OrphanRatio:     ratio,
		ExpandCount:     s.ExpandCount(),
	}
}

// SetWriteCursor 由 recovery 回填写游标。
func (s *LocalFileStorage) SetWriteCursor(v int64) {
	s.Allocator.SetWriteCursor(v)
}

// EnsureCapacity 确保剩余空间足够。
func (s *LocalFileStorage) EnsureCapacity(required int64) error {
	return s.Allocator.EnsureCapacity(required)
}

// EnsureLowWatermark 后台扩容入口（design §47）。
func (s *LocalFileStorage) EnsureLowWatermark() (int64, error) {
	return s.Allocator.EnsureLowWatermark()
}

// DataFileName 返回数据文件名，写入 object_versions.data_file。
func (s *LocalFileStorage) DataFileName() string { return s.fileName }

// SetLiveRecordBytes 更新有效数据字节数（用于 orphan 统计）。
func (s *LocalFileStorage) SetLiveRecordBytes(v int64) { s.liveBytes.Store(v) }

// Path 返回数据文件的绝对路径。
func (s *LocalFileStorage) Path() string { return s.path }

// Close 关闭数据文件。
func (s *LocalFileStorage) Close() error {
	_ = s.Sync()
	if err := s.file.Close(); err != nil {
		return errors.Wrap(errors.CodeStorageError, err)
	}
	return nil
}
