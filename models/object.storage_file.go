package models

import (
	"github.com/favos-me/soss/consts/tablename"
	"github.com/favos-me/soss/consts/types"
)

// StorageFile 数据文件登记（design §76），v1 只有 data.bin 一条记录，
// 但保留 file_id 抽象以便未来演进为多 segment 文件与在线 Compact。
type StorageFile struct {
	ID            int64                   `json:"id"`
	Filename      string                  `json:"filename"`
	CapacityBytes int64                   `json:"capacity_bytes"`
	WriteOffset   int64                   `json:"write_offset"`
	CreateTS      int64                   `json:"created_at"`
	Status        types.StorageFileStatus `json:"status"`
}

// TableName 返回表名。
func (*StorageFile) TableName() string { return tablename.TableNameStorageFiles }

// CommitPutInput 一次对象写入需要持久化的元数据（design §59）。
type CommitPutInput struct {
	BucketID    int64
	ObjectKey   string
	Version     int64 // 候选版本号，事务内仍会按 latest_version 校正
	DataFile    string
	DataOffset  int64
	DataSize    int64
	RecordSize  int64
	Filename    string
	ContentType string
	ETag        string
	Checksum    string
	Now         int64
}

// CommitPutResult 事务提交结果。
type CommitPutResult struct {
	ObjectID int64
	Version  int64
}

// DeleteObjectResult 删除对象结果。
type DeleteObjectResult struct {
	ObjectID        int64
	DeletedVersions int64
}
