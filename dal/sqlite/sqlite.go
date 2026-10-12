// Package sqlite 是元数据的 SQLite 实现（design §14 / §78）。
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	serr "github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/services/monitor"
)

// Storage 元数据库。
//
// 写路径使用"独立写连接 + 进程内互斥"，等价于 BEGIN IMMEDIATE 的效果：
// 同一进程内写事务串行化，避免自相残杀式 SQLITE_BUSY（design §58）。
type Storage struct {
	db        *sql.DB
	writeConn *sql.Conn
	writeMu   sync.Mutex
	path      string
	cfg       config.SQLiteConfig
	logger    *zap.Logger
	metrics   *monitor.Metrics
	alarms    *monitor.Alarms
}

// NewStorage 打开元数据库并应用 PRAGMA。
func NewStorage(path string, cfg config.SQLiteConfig,
	logger *zap.Logger, metrics *monitor.Metrics, alarms *monitor.Alarms) (*Storage, error) {
	// 直接以文件路径作为 DSN：modernc 驱动对每个新建连接应用 _pragma 参数，
	// 保证 busy_timeout / synchronous / foreign_keys 在连接池中的每条连接上都生效。
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(%s)",
		path, cfg.BusyTimeout, cfg.Synchronous)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, serr.Wrap(serr.CodeMetadataError, err)
	}
	// WAL 模式下读并发由连接池承担，写连接单独持有。
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(16)
	db.SetConnMaxLifetime(0)

	ctx := context.Background()
	// journal_mode 是持久属性，显式执行一次确保落盘生效。
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, serr.Wrap(serr.CodeMetadataError, err)
	}
	writeConn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, serr.Wrap(serr.CodeMetadataError, err)
	}
	s := &Storage{
		db:        db,
		writeConn: writeConn,
		path:      path,
		cfg:       cfg,
		logger:    logger,
		metrics:   metrics,
		alarms:    alarms,
	}
	if err := s.Ping(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	logger.Info("sqlite metadata store opened",
		zap.String("path", path),
		zap.String("journal_mode", "WAL"),
		zap.String("synchronous", cfg.Synchronous),
		zap.Int("busy_timeout", cfg.BusyTimeout),
	)
	return s, nil
}

// Ping 检查数据库连通性。
func (s *Storage) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return s.wrapErr(err)
	}
	return nil
}

// DB 暴露底层 *sql.DB，仅供迁移器使用。
func (s *Storage) DB() *sql.DB { return s.db }

// Close 关闭数据库，退出前做一次 WAL checkpoint，便于备份与快速恢复。
func (s *Storage) Close() error {
	if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		s.logger.Warn("wal checkpoint failed", zap.Error(err))
	}
	if s.writeConn != nil {
		_ = s.writeConn.Close()
	}
	if err := s.db.Close(); err != nil {
		return serr.Wrap(serr.CodeMetadataError, err)
	}
	return nil
}

// writeTx 在独立写连接上串行执行一个写事务。
func (s *Storage) writeTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.writeConn.BeginTx(ctx, nil)
	if err != nil {
		return s.wrapErr(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return s.wrapErr(err)
	}
	return nil
}

// wrapErr 把 SQLite 原生错误包装为业务错误，并统计 busy 次数。
func (s *Storage) wrapErr(err error) error {
	if err == nil {
		return nil
	}
	if s.metrics != nil {
		s.metrics.Incr(consts.MetricSQLiteErrors, 1)
		if isBusy(err) {
			s.metrics.Incr(consts.MetricSQLiteBusy, 1)
		}
	}
	if isBusy(err) && s.alarms != nil {
		s.alarms.SQLiteError(err)
	}
	return serr.Wrap(serr.CodeMetadataError, err)
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

// isUniqueViolation 判断是否为唯一约束冲突。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed")
}

// notFound 把 sql.ErrNoRows 转换为指定业务码。
func (s *Storage) notFound(err error, code serr.Code) error {
	if errors.Is(err, sql.ErrNoRows) {
		return serr.NewError(code)
	}
	return s.wrapErr(err)
}
