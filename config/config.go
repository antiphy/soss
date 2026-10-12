// Package config 负责服务配置的加载、默认值填充与校验。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/utils"
	"gopkg.in/yaml.v3"
)

// Size 支持 "1GiB" / "500MiB" 这类写法的容量类型。
type Size int64

// UnmarshalYAML 从 YAML 节点解析容量字符串。
func (s *Size) UnmarshalYAML(v *yaml.Node) error {
	var raw string
	if err := v.Decode(&raw); err != nil {
		var n int64
		if err2 := v.Decode(&n); err2 != nil {
			return err
		}
		*s = Size(n)
		return nil
	}
	n, err := utils.ParseSize(raw)
	if err != nil {
		return fmt.Errorf("parse size %q: %w", raw, err)
	}
	*s = Size(n)
	return nil
}

// Int64 返回字节数。
func (s Size) Int64() int64 { return int64(s) }

// String 返回可读容量。
func (s Size) String() string { return utils.FormatSize(int64(s)) }

// StorageConfig 存储与数据文件配置（design §70）。
type StorageConfig struct {
	Root             string `yaml:"root"`
	DataFile         string `yaml:"data_file"`
	InitialSize      Size   `yaml:"initial_size"`
	ExpandStep       Size   `yaml:"expand_step"`
	LowWatermark     Size   `yaml:"low_watermark"`
	DiskSafetyMargin Size   `yaml:"disk_safety_margin"`

	// Fsync 每次写入 data.bin 后是否执行 Sync，默认 true（design §12）。
	Fsync *bool `yaml:"fsync"`
	// VerifyOnRead 读取对象时是否校验 CRC32C，默认 false（design §54）。
	VerifyOnRead bool `yaml:"verify_on_read"`
}

// CacheConfig 缓存配置（design §21 / §68）。
type CacheConfig struct {
	HotMaxBytes   Size  `yaml:"hot_max_bytes"`
	LRUMaxBytes   Size  `yaml:"lru_max_bytes"`
	LRUMaxObjects int64 `yaml:"lru_max_objects"`
}

// SQLiteConfig 元数据库配置（design §14）。
type SQLiteConfig struct {
	Database    string `yaml:"database"`
	BusyTimeout int    `yaml:"busy_timeout"`
	Synchronous string `yaml:"synchronous"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Listen             string `yaml:"listen"`
	MaxObjectSize      Size   `yaml:"max_object_size"`
	CacheControlHot    string `yaml:"cache_control_hot"`
	CacheControlNormal string `yaml:"cache_control_normal"`
	ReadTimeoutSec     int    `yaml:"read_timeout_sec"`
	WriteTimeoutSec    int    `yaml:"write_timeout_sec"`
	IdleTimeoutSec     int    `yaml:"idle_timeout_sec"`
	// MetricsEnabled 是否暴露 /v1/metrics。
	MetricsEnabled bool `yaml:"metrics_enabled"`
}

// LoggingConfig 日志配置。
type LoggingConfig struct {
	Level   string `yaml:"level"`
	File    string `yaml:"file"`
	ErrFile string `yaml:"err_file"`
	Lang    string `yaml:"lang"`
}

// AuthConfig 鉴权配置（design §90 / §91）。
type AuthConfig struct {
	AdminToken string `yaml:"admin_token"`
	WriteToken string `yaml:"write_token"`
	ReadToken  string `yaml:"read_token"`
	// JWTSecret 管理后台会话 Cookie 的签名密钥；留空时启动时随机生成
	// （安全性更高，代价是重启后已登录会话失效）。
	JWTSecret string `yaml:"jwt_secret"`
	// SessionHour 管理后台会话有效期（小时）。
	SessionHour int `yaml:"session_hour"`
}

// AdminConfig 管理后台（/admin）配置。
type AdminConfig struct {
	// Enabled 是否启用管理后台。
	Enabled bool `yaml:"enabled"`
	// ViewsDir pongo2 模板根目录，相对进程工作目录。
	ViewsDir string `yaml:"views_dir"`
	// StaticDir 静态资源目录，映射到 /static。
	StaticDir string `yaml:"static_dir"`
}

// Config 顶层配置。
type Config struct {
	Storage StorageConfig `yaml:"storage"`
	Cache   CacheConfig   `yaml:"cache"`
	SQLite  SQLiteConfig  `yaml:"sqlite"`
	Server  ServerConfig  `yaml:"server"`
	Logging LoggingConfig `yaml:"logging"`
	Auth    AuthConfig    `yaml:"auth"`
	Admin   AdminConfig   `yaml:"admin"`
}

// DefaultConfig 返回带默认值的配置。
func DefaultConfig() *Config {
	fsync := consts.DefaultStorageFsync
	return &Config{
		Storage: StorageConfig{
			Root:             consts.DefaultStorageRoot,
			DataFile:         consts.DataFileName,
			InitialSize:      Size(consts.DefaultStorageInitialSize),
			ExpandStep:       Size(consts.DefaultStorageExpandStep),
			LowWatermark:     Size(consts.DefaultStorageLowWatermark),
			DiskSafetyMargin: Size(consts.DefaultStorageDiskSafetyMargin),
			Fsync:            &fsync,
			VerifyOnRead:     consts.DefaultVerifyOnRead,
		},
		Cache: CacheConfig{
			HotMaxBytes:   Size(consts.DefaultHotCacheMaxBytes),
			LRUMaxBytes:   Size(consts.DefaultLRUMaxBytes),
			LRUMaxObjects: consts.DefaultLRUMaxObjects,
		},
		SQLite: SQLiteConfig{
			Database:    consts.SQLiteDBName,
			BusyTimeout: consts.DefaultSQLiteBusyTimeout,
			Synchronous: consts.DefaultSQLiteSynchronous,
		},
		Server: ServerConfig{
			Listen:             consts.DefaultListenAddr,
			MaxObjectSize:      Size(consts.DefaultMaxObjectSize),
			CacheControlHot:    consts.DefaultCacheControlHot,
			CacheControlNormal: consts.DefaultCacheControlNormal,
			ReadTimeoutSec:     consts.DefaultReadTimeoutSec,
			WriteTimeoutSec:    consts.DefaultWriteTimeoutSec,
			IdleTimeoutSec:     consts.DefaultIdleTimeoutSec,
			MetricsEnabled:     consts.DefaultMetricsEnabled,
		},
		Logging: LoggingConfig{
			Level: consts.DefaultLogLevel,
			Lang:  consts.DefaultLogLang,
		},
		Admin: AdminConfig{
			Enabled:   consts.DefaultAdminEnabled,
			ViewsDir:  consts.DefaultViewsDir,
			StaticDir: consts.DefaultStaticDir,
		},
	}
}

// Load 从文件读取配置，未配置的字段保留默认值。
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	} else if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	// 无论配置文件是否存在都要校验：JWTSecret 等字段需要在此兜底生成。
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate 校验配置合法性并补齐缺省值。
func (c *Config) Validate() error {
	if c.Storage.Root == "" {
		return errors.New("storage.root is required")
	}
	if c.Storage.DataFile == "" {
		c.Storage.DataFile = consts.DataFileName
	}
	if c.Storage.InitialSize <= 0 {
		c.Storage.InitialSize = Size(consts.DefaultStorageInitialSize)
	}
	if c.Storage.ExpandStep <= 0 {
		c.Storage.ExpandStep = Size(consts.DefaultStorageExpandStep)
	}
	if c.Storage.LowWatermark <= 0 {
		c.Storage.LowWatermark = Size(consts.DefaultStorageLowWatermark)
	}
	if c.Storage.DiskSafetyMargin <= 0 {
		c.Storage.DiskSafetyMargin = Size(consts.DefaultStorageDiskSafetyMargin)
	}
	if c.Storage.Fsync == nil {
		v := consts.DefaultStorageFsync
		c.Storage.Fsync = &v
	}
	if c.Cache.HotMaxBytes <= 0 {
		c.Cache.HotMaxBytes = Size(consts.DefaultHotCacheMaxBytes)
	}
	if c.Cache.LRUMaxBytes <= 0 {
		c.Cache.LRUMaxBytes = Size(consts.DefaultLRUMaxBytes)
	}
	if c.Cache.LRUMaxObjects <= 0 {
		c.Cache.LRUMaxObjects = consts.DefaultLRUMaxObjects
	}
	if c.SQLite.Database == "" {
		c.SQLite.Database = consts.SQLiteDBName
	}
	if c.SQLite.BusyTimeout <= 0 {
		c.SQLite.BusyTimeout = consts.DefaultSQLiteBusyTimeout
	}
	switch c.SQLite.Synchronous {
	case "":
		c.SQLite.Synchronous = consts.DefaultSQLiteSynchronous
	case "FULL", "NORMAL", "OFF", "EXTRA":
	default:
		return fmt.Errorf("sqlite.synchronous must be one of FULL/NORMAL/EXTRA/OFF, got %q", c.SQLite.Synchronous)
	}
	if c.Server.Listen == "" {
		c.Server.Listen = consts.DefaultListenAddr
	}
	if c.Server.MaxObjectSize <= 0 {
		c.Server.MaxObjectSize = Size(consts.DefaultMaxObjectSize)
	}
	if c.Server.CacheControlHot == "" {
		c.Server.CacheControlHot = consts.DefaultCacheControlHot
	}
	if c.Server.CacheControlNormal == "" {
		c.Server.CacheControlNormal = consts.DefaultCacheControlNormal
	}
	if c.Logging.Level == "" {
		c.Logging.Level = consts.DefaultLogLevel
	}
	if c.Logging.Lang == "" {
		c.Logging.Lang = consts.DefaultLogLang
	}
	if c.Auth.SessionHour <= 0 {
		c.Auth.SessionHour = consts.DefaultAdminSessionHour
	}
	// 未显式配置签名密钥时随机生成一个：宁可重启后会话失效，也不留默认密钥。
	if c.Auth.JWTSecret == "" {
		secret, err := randomSecret(consts.RandomSecretBytes)
		if err != nil {
			return err
		}
		c.Auth.JWTSecret = secret
	}
	if c.Admin.ViewsDir == "" {
		c.Admin.ViewsDir = consts.DefaultViewsDir
	}
	if c.Admin.StaticDir == "" {
		c.Admin.StaticDir = consts.DefaultStaticDir
	}
	return nil
}

// randomSecret 生成 n 字节的随机十六进制串。
func randomSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// FsyncEnabled 返回是否开启写入后 fsync。
func (s StorageConfig) FsyncEnabled() bool {
	return s.Fsync == nil || *s.Fsync
}
