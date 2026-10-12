package consts

// 日志字段名（设计文档 §86）。
const (
	LogFieldNameTraceID   = "tid"
	LogFieldNameBucket    = "bucket"
	LogFieldNameObjectKey = "object_key"
	LogFieldNameVersion   = "version"
	LogFieldNameOperation = "operation"
	LogFieldNameSize      = "size"
	LogFieldNameOffset    = "offset"
	LogFieldNameLatency   = "latency"
	LogFieldNameCache     = "cache"
	LogFieldNameError     = "err"
	LogFieldNameRequest   = "req"
	LogFieldNameResponse  = "res"
)

// 操作名，写入 operation 日志字段。
const (
	OperationPutBucket    = "PUT_BUCKET"
	OperationGetBucket    = "GET_BUCKET"
	OperationListBucket   = "LIST_BUCKET"
	OperationPatchBucket  = "PATCH_BUCKET"
	OperationDeleteBucket = "DELETE_BUCKET"

	OperationPutObject      = "PUT"
	OperationGetObject      = "GET"
	OperationHeadObject     = "HEAD"
	OperationDeleteObject   = "DELETE"
	OperationListVersions   = "LIST_VERSIONS"
	OperationHotWarmUp      = "HOT_WARMUP"
	OperationExpand         = "EXPAND"
	OperationCapacityCheck  = "CAPACITY_CHECK"
	OperationStartupRecover = "RECOVERY"
)

// 缓存命中标记，写入 cache 日志字段。
const (
	CacheHotHit   = "HOT_HIT"
	CacheHotMiss  = "HOT_MISS"
	CacheLRUHit   = "LRU_HIT"
	CacheLRUMiss  = "LRU_MISS"
	CacheDiskRead = "DISK"
)
