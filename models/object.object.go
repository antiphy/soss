package models

import (
	"github.com/favos-me/soss/consts/tablename"
)

// Object 逻辑对象，一个 object_key 一行（design §16）。
type Object struct {
	ID            int64  `json:"id"`
	BucketID      int64  `json:"bucket_id"`
	ObjectKey     string `json:"object_key"`
	LatestVersion int64  `json:"latest_version"`
	CreateTS      int64  `json:"created_at"`
	UpdateTS      int64  `json:"updated_at"`
}

// TableName 返回表名。
func (*Object) TableName() string { return tablename.TableNameObjects }

// ObjectPutReq 上传对象请求。
type ObjectPutReq struct {
	Bucket      string
	ObjectKey   string
	Filename    string
	ContentType string
	Body        []byte
}

// ObjectPutRes 上传对象响应。
type ObjectPutRes struct {
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"object_key"`
	Version     int64  `json:"version"`
	Size        int64  `json:"size"`
	ETag        string `json:"etag"`
	ContentType string `json:"content_type"`
	Filename    string `json:"filename,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

// ObjectGetRes 读取对象结果，控制器据此写响应头与 body。
type ObjectGetRes struct {
	Bucket        string
	ObjectKey     string
	Version       int64
	LatestVersion int64
	Size          int64
	Data          []byte
	ContentType   string
	ETag          string
	Filename      string
	Checksum      string
	CreatedAt     int64
	CacheFlag     string
	// CacheControl 由 bucket class 决定的响应缓存策略（design §56）。
	CacheControl string
}

// ObjectDeleteRes 删除对象响应。
type ObjectDeleteRes struct {
	Bucket     string `json:"bucket"`
	ObjectKey  string `json:"object_key"`
	DeletedVer int64  `json:"deleted_versions"`
}

// ObjectItem 对象列表项（管理后台 / 列举接口使用）。
type ObjectItem struct {
	ObjectKey   string `json:"object_key"`
	Version     int64  `json:"version"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type,omitempty"`
	Filename    string `json:"filename,omitempty"`
	ETag        string `json:"etag,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

// ObjectListRes 对象列表响应。
type ObjectListRes struct {
	Bucket string       `json:"bucket"`
	Prefix string       `json:"prefix,omitempty"`
	Items  []ObjectItem `json:"items"`
	Total  int          `json:"total"`
	// Truncated 为 true 表示结果被 limit 截断，还有更多对象未返回。
	Truncated bool `json:"truncated"`
}
