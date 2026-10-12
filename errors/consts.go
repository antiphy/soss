package errors

// Code 业务错误码。数值不复用 HTTP 状态码语义，HTTP 状态码由 HTTPStatus() 单独映射。
type Code int

const (
	// CodeOK 成功。
	CodeOK Code = 0

	CodeInvalidArgument Code = 40001
	CodeObjectTooLarge  Code = 41301

	CodeBucketNotFound  Code = 40401
	CodeObjectNotFound  Code = 40402
	CodeVersionNotFound Code = 40403

	CodeBucketExists    Code = 40901
	CodeBucketNotEmpty  Code = 40902
	CodeVersionConflict Code = 40903

	CodeDiskFull              Code = 50701
	CodeCacheCapacityExceeded Code = 50702

	CodeStorageError     Code = 50001
	CodeMetadataError    Code = 50002
	CodeInternalError    Code = 50003
	CodeStorageCorrupted Code = 50301
	CodeServiceReadOnly  Code = 50302

	CodeUnauthorized     Code = 40101
	CodePermissionDenied Code = 40301
)

// 错误码对应的字符串 code，与 API 错误体中的 code 字段一致（设计文档 §69）。
const (
	NameInvalidArgument       = "INVALID_ARGUMENT"
	NameObjectTooLarge        = "OBJECT_TOO_LARGE"
	NameBucketNotFound        = "BUCKET_NOT_FOUND"
	NameObjectNotFound        = "OBJECT_NOT_FOUND"
	NameVersionNotFound       = "VERSION_NOT_FOUND"
	NameBucketExists          = "BUCKET_EXISTS"
	NameBucketNotEmpty        = "BUCKET_NOT_EMPTY"
	NameVersionConflict       = "VERSION_CONFLICT"
	NameDiskFull              = "DISK_FULL"
	NameCacheCapacityExceeded = "CACHE_CAPACITY_EXCEEDED"
	NameStorageError          = "STORAGE_ERROR"
	NameMetadataError         = "METADATA_ERROR"
	NameInternalError         = "INTERNAL_ERROR"
	NameStorageCorrupted      = "STORAGE_CORRUPTED"
	NameServiceReadOnly       = "SERVICE_READ_ONLY"
	NameUnauthorized          = "UNAUTHORIZED"
	NamePermissionDenied      = "PERMISSION_DENIED"
)

// 多语种文案（EN / ZH）。
const (
	MsgInvalidArgumentEN  = "invalid argument"
	MsgInvalidArgumentZH  = "参数错误"
	MsgObjectTooLargeEN   = "object is too large"
	MsgObjectTooLargeZH   = "对象超过大小上限"
	MsgBucketNotFoundEN   = "bucket does not exist"
	MsgBucketNotFoundZH   = "存储桶不存在"
	MsgObjectNotFoundEN   = "object does not exist"
	MsgObjectNotFoundZH   = "对象不存在"
	MsgVersionNotFoundEN  = "version does not exist"
	MsgVersionNotFoundZH  = "对象版本不存在"
	MsgBucketExistsEN     = "bucket already exists"
	MsgBucketExistsZH     = "存储桶已存在"
	MsgBucketNotEmptyEN   = "bucket contains objects"
	MsgBucketNotEmptyZH   = "存储桶非空，不能删除"
	MsgVersionConflictEN  = "version conflict"
	MsgVersionConflictZH  = "对象版本冲突"
	MsgDiskFullEN         = "storage capacity is insufficient"
	MsgDiskFullZH         = "存储空间不足"
	MsgCacheCapacityEN    = "hot cache capacity exceeded"
	MsgCacheCapacityZH    = "内存缓存容量不足"
	MsgStorageErrorEN     = "storage engine error"
	MsgStorageErrorZH     = "存储引擎错误"
	MsgMetadataErrorEN    = "metadata database error"
	MsgMetadataErrorZH    = "元数据库错误"
	MsgInternalErrorEN    = "internal server error"
	MsgInternalErrorZH    = "服务内部错误"
	MsgStorageCorruptedEN = "storage data corrupted, service is in read-only safe mode"
	MsgStorageCorruptedZH = "存储数据损坏，服务已进入只读安全模式"
	MsgServiceReadOnlyEN  = "service is read-only"
	MsgServiceReadOnlyZH  = "服务当前只读"
	MsgUnauthorizedEN     = "unauthorized"
	MsgUnauthorizedZH     = "未授权"
	MsgPermissionDeniedEN = "permission denied"
	MsgPermissionDeniedZH = "权限不足"
)

type codeMeta struct {
	name string
	http int
	en   string
	zh   string
}

var codeRegistry = map[Code]codeMeta{
	CodeInvalidArgument:       {NameInvalidArgument, 400, MsgInvalidArgumentEN, MsgInvalidArgumentZH},
	CodeObjectTooLarge:        {NameObjectTooLarge, 413, MsgObjectTooLargeEN, MsgObjectTooLargeZH},
	CodeBucketNotFound:        {NameBucketNotFound, 404, MsgBucketNotFoundEN, MsgBucketNotFoundZH},
	CodeObjectNotFound:        {NameObjectNotFound, 404, MsgObjectNotFoundEN, MsgObjectNotFoundZH},
	CodeVersionNotFound:       {NameVersionNotFound, 404, MsgVersionNotFoundEN, MsgVersionNotFoundZH},
	CodeBucketExists:          {NameBucketExists, 409, MsgBucketExistsEN, MsgBucketExistsZH},
	CodeBucketNotEmpty:        {NameBucketNotEmpty, 409, MsgBucketNotEmptyEN, MsgBucketNotEmptyZH},
	CodeVersionConflict:       {NameVersionConflict, 409, MsgVersionConflictEN, MsgVersionConflictZH},
	CodeDiskFull:              {NameDiskFull, 507, MsgDiskFullEN, MsgDiskFullZH},
	CodeCacheCapacityExceeded: {NameCacheCapacityExceeded, 507, MsgCacheCapacityEN, MsgCacheCapacityZH},
	CodeStorageError:          {NameStorageError, 500, MsgStorageErrorEN, MsgStorageErrorZH},
	CodeMetadataError:         {NameMetadataError, 500, MsgMetadataErrorEN, MsgMetadataErrorZH},
	CodeInternalError:         {NameInternalError, 500, MsgInternalErrorEN, MsgInternalErrorZH},
	CodeStorageCorrupted:      {NameStorageCorrupted, 503, MsgStorageCorruptedEN, MsgStorageCorruptedZH},
	CodeServiceReadOnly:       {NameServiceReadOnly, 503, MsgServiceReadOnlyEN, MsgServiceReadOnlyZH},
	CodeUnauthorized:          {NameUnauthorized, 401, MsgUnauthorizedEN, MsgUnauthorizedZH},
	CodePermissionDenied:      {NamePermissionDenied, 403, MsgPermissionDeniedEN, MsgPermissionDeniedZH},
}
