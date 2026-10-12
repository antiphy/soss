package models

import (
	"github.com/favos-me/soss/consts/tablename"
	"github.com/favos-me/soss/consts/types"
)

// Bucket 存储桶（design §15）。
type Bucket struct {
	ID       int64             `json:"id"`
	Name     string            `json:"name"`
	Class    types.BucketClass `json:"class"`
	CreateTS int64             `json:"created_at"`
	UpdateTS int64             `json:"updated_at"`
}

// TableName 返回表名。
func (*Bucket) TableName() string { return tablename.TableNameBuckets }

// BucketCreateReq 创建桶请求（design §28）。
type BucketCreateReq struct {
	Name  string            `json:"name"`
	Class types.BucketClass `json:"class"`
}

// BucketUpdateReq 修改桶请求，仅 class 可改（design §31）。
type BucketUpdateReq struct {
	Class *types.BucketClass `json:"class"`
}

// BucketView 带对象统计的桶视图。
type BucketView struct {
	Bucket
	ObjectCount int64 `json:"object_count"`
	TotalBytes  int64 `json:"total_bytes"`
}
