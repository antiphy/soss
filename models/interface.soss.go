package models

import (
	"context"

	"github.com/favos-me/soss/consts/types"
)

// SossDataSource 元数据数据源契约，由 dal/sqlite 实现，logic 仅依赖本接口。
type SossDataSource interface {
	// ---------- bucket ----------
	CreateBucket(ctx context.Context, b *Bucket) error
	GetBucketByName(ctx context.Context, name string) (Bucket, error)
	GetBucketByID(ctx context.Context, id int64) (Bucket, error)
	ListBuckets(ctx context.Context) ([]Bucket, error)
	UpdateBucketClass(ctx context.Context, id int64, class types.BucketClass) error
	DeleteBucket(ctx context.Context, id int64) error
	CountObjects(ctx context.Context, bucketID int64) (int64, error)
	SumObjectPayloadSize(ctx context.Context, bucketID int64) (int64, error)

	// ---------- object / version 读 ----------
	// LookupVersion version <= 0 表示读取 latest。
	LookupVersion(ctx context.Context, bucketID int64, bucketName, objectKey string, version int64) (VersionView, error)
	ListVersions(ctx context.Context, bucketID int64, objectKey string) ([]VersionMeta, error)
	ListLatestVersions(ctx context.Context, bucketID int64, bucketName string) ([]VersionView, error)

	// ---------- object / version 写 ----------
	// CommitPut 在一个事务内完成 object 创建/更新、版本插入、latest_version 更新、
	// storage_files.write_offset 推进（design §36 / §58）。
	CommitPut(ctx context.Context, in CommitPutInput) (CommitPutResult, error)
	// DeleteObject 删除对象及其全部版本元数据，不触碰 data.bin（design §42）。
	DeleteObject(ctx context.Context, bucketID int64, objectKey string) (DeleteObjectResult, error)

	// ---------- 存储文件登记 / 恢复 ----------
	EnsureStorageFile(ctx context.Context, filename string, capacity int64, now int64) (StorageFile, error)
	UpdateStorageFileCapacity(ctx context.Context, filename string, capacity, writeOffset int64) error
	// MaxRecordEnd 返回 MAX(data_offset + record_size)，用于重建 write_cursor（design §11）。
	MaxRecordEnd(ctx context.Context) (int64, error)
	// SumRecordSize 返回全部有效版本 record_size 之和，用于 orphan 统计（design §44）。
	SumRecordSize(ctx context.Context) (int64, error)
	// CheckCorruption 检查是否存在指向数据文件之外的元数据记录（design §73）。
	CheckCorruption(ctx context.Context, capacity int64) ([]ObjectVersion, error)

	// Close 关闭数据源。
	Close() error
}
