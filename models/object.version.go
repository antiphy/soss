package models

import (
	"github.com/favos-me/soss/consts/tablename"
)

// ObjectVersion 对象版本（design §17）。
//
// data_offset 语义：record 起始位置（header 第一个字节），payload 位于
// data_offset + HeaderSize。这样 write_cursor = MAX(data_offset + record_size) 精确成立。
type ObjectVersion struct {
	ID          int64  `json:"id"`
	ObjectID    int64  `json:"object_id"`
	Version     int64  `json:"version"`
	DataFile    string `json:"data_file"`
	DataOffset  int64  `json:"data_offset"`
	DataSize    int64  `json:"data_size"`
	RecordSize  int64  `json:"record_size"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	ETag        string `json:"etag,omitempty"`
	Checksum    string `json:"checksum,omitempty"`
	CreateTS    int64  `json:"created_at"`
}

// TableName 返回表名。
func (*ObjectVersion) TableName() string { return tablename.TableNameObjectVersion }

// VersionView 读取路径使用的联表视图（design §62 / §63）。
type VersionView struct {
	BucketID      int64
	BucketName    string
	ObjectID      int64
	ObjectKey     string
	LatestVersion int64
	Version       int64
	DataFile      string
	DataOffset    int64
	DataSize      int64
	RecordSize    int64
	Filename      string
	ContentType   string
	ETag          string
	Checksum      string
	CreateTS      int64
}

// VersionMeta 版本列表项（design §40）。
type VersionMeta struct {
	Version   int64  `json:"version"`
	Size      int64  `json:"size"`
	ETag      string `json:"etag,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// VersionListRes 版本列表响应。
type VersionListRes struct {
	Bucket   string        `json:"bucket"`
	Object   string        `json:"object"`
	Versions []VersionMeta `json:"versions"`
	Total    int           `json:"total"`
}
