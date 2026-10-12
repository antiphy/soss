package consts

// 配置项默认值：与 config.yaml 的键一一对应，config 包只做解析与校验，不再内联字面量。

// 二进制容量单位，供下面的容量常量组合使用。
const (
	KiB int64 = 1 << 10
	MiB int64 = 1 << 20
	GiB int64 = 1 << 30
	TiB int64 = 1 << 40
)

// 顶层：配置文件位置。
const (
	// DefaultConfigPath 默认配置文件路径。
	DefaultConfigPath = "config.yaml"
)

// storage.*：数据文件与容量策略。
const (
	// DefaultStorageRoot 存储根目录。
	DefaultStorageRoot = "./data/oss"
	// DataFileName 对象数据文件名，同时是 storage.data_file 的默认值。
	DataFileName = "data.bin"
	// DefaultStorageInitialSize 数据文件初始大小。
	DefaultStorageInitialSize = 1 * GiB
	// DefaultStorageExpandStep 每次扩容步长。
	DefaultStorageExpandStep = 1 * GiB
	// DefaultStorageLowWatermark 剩余容量低于该值时触发扩容。
	DefaultStorageLowWatermark = 500 * MiB
	// DefaultStorageDiskSafetyMargin 扩容时预留的磁盘安全空间。
	DefaultStorageDiskSafetyMargin = 1 * GiB
	// DefaultStorageFsync 写入后是否 fsync。
	DefaultStorageFsync = true
	// DefaultVerifyOnRead 读取对象时是否校验 CRC32C。
	DefaultVerifyOnRead = false
)

// cache.*：HOT 与 LRU 缓存容量。
const (
	// DefaultHotCacheMaxBytes HOT 缓存字节上限。
	DefaultHotCacheMaxBytes = 8 * GiB
	// DefaultLRUMaxBytes LRU 缓存字节上限。
	DefaultLRUMaxBytes = 2 * GiB
	// DefaultLRUMaxObjects LRU 缓存对象数上限。
	DefaultLRUMaxObjects = 500000
)

// sqlite.*：元数据库。
const (
	// SQLiteDBName 元数据库文件名，同时是 sqlite.database 的默认值。
	SQLiteDBName = "oss.db"
	// DefaultSQLiteBusyTimeout 忙等待超时（毫秒）。
	DefaultSQLiteBusyTimeout = 5000
	// DefaultSQLiteSynchronous journal 同步模式。
	DefaultSQLiteSynchronous = "FULL"
)

// server.*：HTTP 服务。
const (
	// DefaultListenAddr 监听地址。
	DefaultListenAddr = "0.0.0.0:8080"
	// DefaultMaxObjectSize 单个对象大小上限。
	DefaultMaxObjectSize = 100 * MiB
	// DefaultCacheControlHot HOT 桶对象的 Cache-Control（设计文档 §56）。
	DefaultCacheControlHot = "public, max-age=31536000, immutable"
	// DefaultCacheControlNormal 普通桶对象的 Cache-Control（设计文档 §56）。
	DefaultCacheControlNormal = "public, max-age=300"
	// DefaultReadTimeoutSec 读超时（秒）。
	DefaultReadTimeoutSec = 60
	// DefaultWriteTimeoutSec 写超时（秒）。
	DefaultWriteTimeoutSec = 300
	// DefaultIdleTimeoutSec 空闲连接超时（秒）。
	DefaultIdleTimeoutSec = 120
	// DefaultMetricsEnabled 是否默认暴露 /v1/metrics。
	DefaultMetricsEnabled = true
)

// logging.*：日志。
const (
	// DefaultLogLevel 默认日志级别。
	DefaultLogLevel = "info"
	// DefaultLogLang 错误文案语言。
	DefaultLogLang = "en"
)

// auth.*：鉴权。
const (
	// DefaultAdminSessionHour 管理员会话默认有效期（小时）。
	DefaultAdminSessionHour = 12
	// RandomSecretBytes jwt_secret 缺省时生成的随机密钥字节数。
	RandomSecretBytes = 32
	// 令牌（admin/write/read）没有默认值：留空即拒绝相应操作，必须由配置提供。
)

// admin.*：管理后台。
const (
	// DefaultAdminEnabled 是否默认启用管理后台。
	DefaultAdminEnabled = true
	// DefaultViewsDir pongo2 模板根目录。
	DefaultViewsDir = "views"
	// DefaultStaticDir 静态资源目录。
	DefaultStaticDir = "static"
)
