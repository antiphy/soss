package consts

// 监控指标名（设计文档 §87）。
const (
	MetricRequestsTotal    = "oss_requests_total"
	MetricRequestErrors    = "oss_request_errors_total"
	MetricRequestLatencyMS = "oss_request_duration_ms_sum"

	MetricHotCacheHit     = "oss_hot_cache_hit_total"
	MetricHotCacheMiss    = "oss_hot_cache_miss_total"
	MetricHotCacheBytes   = "oss_hot_cache_bytes"
	MetricHotCacheObjects = "oss_hot_cache_objects"

	MetricLRUHit      = "oss_lru_hit_total"
	MetricLRUMiss     = "oss_lru_miss_total"
	MetricLRUEviction = "oss_lru_eviction_total"
	MetricLRUBytes    = "oss_lru_bytes"
	MetricLRUObjects  = "oss_lru_objects"

	MetricDataFileSize     = "oss_data_file_size_bytes"
	MetricDataWriteCursor  = "oss_data_write_cursor_bytes"
	MetricDataFreeCapacity = "oss_data_free_capacity_bytes"
	MetricDiskFreeBytes    = "oss_disk_free_bytes"
	MetricOrphanBytes      = "oss_orphan_bytes"
	MetricLiveRecordBytes  = "oss_live_record_bytes"
	MetricExpandTotal      = "oss_expand_total"

	MetricSQLiteErrors     = "oss_sqlite_errors_total"
	MetricSQLiteBusy       = "oss_sqlite_busy_total"
	MetricStorageReadError = "oss_storage_read_error_total"
	MetricAlarmTotal       = "oss_alarm_total"

	MetricPutBytesTotal = "oss_put_bytes_total"
	MetricGetBytesTotal = "oss_get_bytes_total"
)
