package consts

// 应用级常量。配置项默认值见 consts/config.go。
const (
	AppName    = "soss"
	AppVersion = "1.0.0"
)

// 存储目录下的固定子目录 / 文件名（data.bin、oss.db 的默认值在 config.go）。
const (
	LockDirName     = "lock"
	LockFileName    = "writer.lock"
	TmpDirName      = "tmp"
	RecoveryDirName = "recovery"
)

// HTTP 相关常量。
const (
	HeaderObjectVersion      = "X-Object-Version"
	HeaderFilename           = "X-Filename"
	HeaderRequestID          = "X-Request-Id"
	HeaderCacheControl       = "Cache-Control"
	HeaderETag               = "ETag"
	HeaderContentDisposition = "Content-Disposition"
	HeaderIfNoneMatch        = "If-None-Match"
	HeaderLastModified       = "Last-Modified"
	HeaderAuthorization      = "Authorization"
)

// Object key 规范（设计文档 §82）。
const (
	// MaxObjectKeyBytes object key 最大字节数（UTF-8）。
	MaxObjectKeyBytes = 1024
)

// 上下文 key。
const (
	// ContextKeyRequestID 存放于 echo.Context 中的 request id。
	ContextKeyRequestID = "request_id"
	// ContextKeyLogger 存放于 context.Context 中的 *zap.Logger。
	ContextKeyLogger = "logger"
	// ContextKeyAdminUser 登录后的管理员用户名，由 AdminSession 中间件写入。
	ContextKeyAdminUser = "admin_user"
)

// 管理后台（/admin）相关常量。
const (
	// AdminPathPrefix 管理后台路由前缀。
	AdminPathPrefix = "/admin"
	// AdminAPIPrefix 管理后台 JSON API 前缀。
	AdminAPIPrefix = "/admin/api"
	// AdminLoginPath 登录页路径。
	AdminLoginPath = "/admin/login"
	// AdminCookieName 管理员会话 Cookie 名称。
	AdminCookieName = "soss_admin"
	// AdminCookiePath Cookie 生效路径，只跟随 /admin 下的请求。
	AdminCookiePath = "/admin"
	// MaxAdminListObjects 管理后台单次最多返回的对象条数。
	MaxAdminListObjects = 500
	// DefaultViewLang 模板语言目录（SKILL.md §2：views/<lang>/）。
	DefaultViewLang = "zh"
)

// 容量 Worker 与 warm-up 的默认节奏。
const (
	// CapacityCheckIntervalSec 后台容量巡检周期（秒）。
	CapacityCheckIntervalSec = 30
	// CapacitySignalBuffer 容量触发信号通道缓冲。
	CapacitySignalBuffer = 64
	// WarmUpBatchSize HOT 桶 warm-up 每批加载对象数。
	WarmUpBatchSize = 256
)
