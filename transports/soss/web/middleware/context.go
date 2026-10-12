// Package middleware 提供 HTTP 传输层的横切中间件。
package middleware

import (
	"context"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/utils"
)

type contextKey string

// keyLogger 是 *zap.Logger 在 request context 中的键（具名类型，避免与字符串键冲突）。
const keyLogger contextKey = contextKey(consts.ContextKeyLogger)

// RequestID 注入 request id（tid），并把带 tid 的 logger 放入 context。
func RequestID(logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rid := c.Request().Header.Get(consts.HeaderRequestID)
			if rid == "" {
				rid = utils.NewXID()
			}
			c.Set(consts.ContextKeyRequestID, rid)
			c.Response().Header().Set(consts.HeaderRequestID, rid)

			scoped := logger.With(zap.String(consts.LogFieldNameTraceID, rid))
			req := c.Request()
			ctx := context.WithValue(req.Context(), keyLogger, scoped)
			c.SetRequest(req.WithContext(ctx))
			// 同时放进 echo context，便于控制器直接取用。
			c.Set(consts.ContextKeyLogger, scoped)
			return next(c)
		}
	}
}

// RequestLogger 记录访问日志与请求指标。
func RequestLogger(logger *zap.Logger, incr func(name string, delta int64)) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			elapsed := time.Since(start).Milliseconds()

			incr(consts.MetricRequestsTotal, 1)
			incr(consts.MetricRequestLatencyMS, elapsed)
			if err != nil {
				incr(consts.MetricRequestErrors, 1)
			}
			fields := []zap.Field{
				zap.String("method", c.Request().Method),
				zap.String("path", c.Request().URL.Path),
				zap.Int("status", c.Response().Status),
				zap.Int64(consts.LogFieldNameLatency, elapsed),
			}
			if err != nil {
				fields = append(fields, zap.Error(err))
				Logger(c, logger).Error("request failed", fields...)
				return err
			}
			Logger(c, logger).Info("request", fields...)
			return nil
		}
	}
}

// Recover 捕获 panic，避免单个请求打挂整个进程。
func Recover(logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			defer func() {
				if r := recover(); r != nil {
					Logger(c, logger).Error("panic recovered", zap.Any("panic", r))
					_ = c.JSON(500, map[string]string{
						"code":    "INTERNAL_ERROR",
						"message": "internal server error",
					})
				}
			}()
			return next(c)
		}
	}
}

// Logger 从 request context 取出带 tid 的 logger，取不到时回退到全局 logger。
func Logger(c echo.Context, fallback *zap.Logger) *zap.Logger {
	if c == nil {
		return fallback
	}
	if l, ok := c.Request().Context().Value(keyLogger).(*zap.Logger); ok && l != nil {
		return l
	}
	return fallback
}
