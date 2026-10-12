// Command soss 是一个轻量级本地对象存储服务（SOSS）。
//
// 子命令：
//
//	start    启动服务（恢复存储状态 -> 后台任务 -> HTTP 服务）
//	init     仅初始化存储目录、元数据与数据文件
//	version  打印版本信息
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/dal/sqlite"
	sosserrors "github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/logic"
	"github.com/favos-me/soss/services/monitor"
	"github.com/favos-me/soss/services/recovery"
	"github.com/favos-me/soss/services/storage"
	"github.com/favos-me/soss/transports/soss/web"
)

//go:embed scripts
var scriptsFS embed.FS

// app 持有全部运行时资源，便于统一释放。
type app struct {
	cfg     *config.Config
	logger  *zap.Logger
	metrics *monitor.Metrics
	alarms  *monitor.Alarms
	ds      *sqlite.Storage
	store   storage.StorageEngine
	unlock  func()
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	switch cmd {
	case "start":
		if err := runStart(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "start failed: %v\n", err)
			os.Exit(1)
		}
	case "init":
		if err := runInit(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Printf("%s %s (go %s)\n", consts.AppName, consts.AppVersion, "1.27")
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Printf("usage: %s <start|init|version> [-c config.yaml] [-dev]\n", consts.AppName)
}

func runStart(args []string) error {
	flags := flag.NewFlagSet("start", flag.ExitOnError)
	configPath := flags.String("c", consts.DefaultConfigPath, "config file path")
	dev := flags.Bool("dev", false, "dev mode: load migration scripts from ./scripts")
	if err := flags.Parse(args); err != nil {
		return err
	}

	a, err := bootstrap(*configPath, *dev)
	if err != nil {
		return err
	}
	defer a.close()

	report, err := a.recover(context.Background())
	if err != nil {
		return err
	}
	if report.Corrupted {
		a.logger.Error("storage corruption detected, service starts in READ-ONLY safe mode",
			zap.Int("corrupted_records", report.CorruptedCount))
	}

	logic.Init(a.cfg, a.logger, a.ds, a.store, a.metrics, a.alarms)
	logic.Logic.Start(report)

	e := web.NewRouters(web.Deps{Config: a.cfg, Logger: a.logger, Metrics: a.metrics})

	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("http server listening", zap.String("listen", a.cfg.Server.Listen))
		if err := e.Start(a.cfg.Server.Listen); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-quit:
		a.logger.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-errCh:
		a.logger.Error("http server failed", zap.Error(err))
		// 后台任务必须先停，否则会在 a.close() 关闭存储后继续读写。
		logic.Logic.Stop()
		return err
	}

	// 优雅退出：先停 HTTP，再停后台任务，最后刷盘关闭存储与元数据（SKILL.md §13）。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		a.logger.Warn("http shutdown failed", zap.Error(err))
	}
	logic.Logic.Stop()
	return nil
}

func runInit(args []string) error {
	flags := flag.NewFlagSet("init", flag.ExitOnError)
	configPath := flags.String("c", consts.DefaultConfigPath, "config file path")
	dev := flags.Bool("dev", false, "dev mode: load migration scripts from ./scripts")
	if err := flags.Parse(args); err != nil {
		return err
	}

	a, err := bootstrap(*configPath, *dev)
	if err != nil {
		return err
	}
	defer a.close()

	report, err := a.recover(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("init done: root=%s data_file=%s capacity=%d write_cursor=%d read_only=%v\n",
		a.cfg.Storage.Root, report.DataFile, report.CapacityBytes, report.WriteCursor, report.ReadOnly)
	return nil
}

// bootstrap 读取配置、建目录、打开元数据与数据文件，并执行迁移（design §71）。
func bootstrap(configPath string, dev bool) (*app, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	sosserrors.SetLang(cfg.Logging.Lang)

	logger, err := newLogger(cfg)
	if err != nil {
		return nil, err
	}

	root := cfg.Storage.Root
	for _, dir := range []string{root, filepath.Join(root, consts.TmpDirName),
		filepath.Join(root, consts.RecoveryDirName), filepath.Join(root, consts.LockDirName)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	metrics := monitor.NewMetrics()
	alarms := monitor.NewAlarms(logger, metrics)

	dsPath := filepath.Join(root, cfg.SQLite.Database)
	ds, err := sqlite.NewStorage(dsPath, cfg.SQLite, logger, metrics, alarms)
	if err != nil {
		return nil, err
	}
	if err := sqlite.NewMigrator(ds.DB(), getScriptsFS(dev), logger).Init(context.Background()); err != nil {
		_ = ds.Close()
		return nil, err
	}

	store, err := storage.Open(root, cfg.Storage, logger, metrics, alarms)
	if err != nil {
		_ = ds.Close()
		return nil, err
	}

	return &app{
		cfg:     cfg,
		logger:  logger,
		metrics: metrics,
		alarms:  alarms,
		ds:      ds,
		store:   store,
		unlock:  acquireWriterLock(root, logger),
	}, nil
}

// recover 执行启动恢复流程。
func (a *app) recover(ctx context.Context) (*recovery.Report, error) {
	return recovery.Run(ctx, a.ds, a.store, a.cfg, a.logger, a.metrics, a.alarms)
}

func (a *app) close() {
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			a.logger.Warn("close data file failed", zap.Error(err))
		}
	}
	if a.ds != nil {
		if err := a.ds.Close(); err != nil {
			a.logger.Warn("close metadata store failed", zap.Error(err))
		}
	}
	if a.unlock != nil {
		a.unlock()
	}
	if a.logger != nil {
		_ = a.logger.Sync()
	}
}

// getScriptsFS 返回迁移脚本文件系统：dev 模式回退到运行时目录（SKILL.md §11）。
func getScriptsFS(dev bool) fs.FS {
	if dev {
		if info, err := os.Stat("scripts"); err == nil && info.IsDir() {
			return os.DirFS("scripts")
		}
	}
	sub, err := fs.Sub(scriptsFS, "scripts")
	if err != nil {
		panic("embed scripts missing: " + err.Error())
	}
	return sub
}

// acquireWriterLock 尝试以独占方式创建 writer.lock，保证"单活写入"模型（design §5）。
func acquireWriterLock(root string, logger *zap.Logger) func() {
	path := filepath.Join(root, consts.LockDirName, consts.LockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Warn("writer.lock already exists, make sure only one writer is active",
			zap.String("path", path),
			zap.Error(err),
		)
		return nil
	}
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + " " + time.Now().Format(time.RFC3339) + "\n")
	_ = f.Close()
	return func() {
		_ = os.Remove(path)
	}
}

func newLogger(cfg *config.Config) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Logging.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}
	zapCfg := zap.NewProductionConfig()
	zapCfg.Level = zap.NewAtomicLevelAt(level)
	zapCfg.EncoderConfig.TimeKey = "ts"
	zapCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	outputs := []string{"stdout"}
	if cfg.Logging.File != "" {
		outputs = []string{cfg.Logging.File}
	}
	zapCfg.OutputPaths = outputs
	zapCfg.ErrorOutputPaths = outputs
	return zapCfg.Build()
}
