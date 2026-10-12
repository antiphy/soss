package tablename

// 表名集中定义，禁止在业务代码里散落字面量。
const (
	TableNameBuckets       = "buckets"
	TableNameObjects       = "objects"
	TableNameObjectVersion = "object_versions"
	TableNameStorageFiles  = "storage_files"
)
