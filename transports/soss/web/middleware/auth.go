package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/errors"
)

// 权限级别（design §91）。
type role int

const (
	roleRead role = iota + 1
	roleWrite
	roleAdmin
)

// Auth 基于 Bearer Token 的鉴权中间件。
//
// 未配置任何 token 时视为开发模式：放行但打印 WARN 日志。
func Auth(cfg *config.AuthConfig, logger *zap.Logger) echo.MiddlewareFunc {
	devMode := cfg.AdminToken == "" && cfg.WriteToken == "" && cfg.ReadToken == ""
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if devMode {
				return next(c)
			}
			need := requiredRole(c.Request().Method, routePath(c))
			token := bearerToken(c.Request().Header.Get(consts.HeaderAuthorization))
			if !satisfy(cfg, need, token) {
				Logger(c, logger).Warn("auth rejected",
					zap.String("method", c.Request().Method),
					zap.String("path", c.Request().URL.Path),
				)
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    errors.CodeName(errors.CodeUnauthorized),
					"message": errors.Message(errors.CodeUnauthorized),
				})
			}
			return next(c)
		}
	}
}

// requiredRole 按方法与路由判定所需权限（design §91）。
//
// 注意：桶的增删改查已迁到 /admin（走会话鉴权），对象的读写由
// RequireWriteToken 单独把关，因此本中间件现在只服务于 /v1/metrics 这类运营接口。
func requiredRole(method, path string) role {
	switch method {
	case http.MethodGet, http.MethodHead:
		if strings.HasSuffix(path, "/metrics") {
			return roleAdmin
		}
		return roleRead
	case http.MethodPut, http.MethodPatch, http.MethodDelete:
		return roleWrite
	}
	return roleAdmin
}

func routePath(c echo.Context) string {
	if p := c.Path(); p != "" {
		return p
	}
	return c.Request().URL.Path
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return strings.TrimSpace(header)
}

// satisfy admin token 可通吃，write token 覆盖写，read token 只读。
func satisfy(cfg *config.AuthConfig, need role, token string) bool {
	if token == "" {
		return false
	}
	switch need {
	case roleAdmin:
		return cfg.AdminToken != "" && token == cfg.AdminToken
	case roleWrite:
		return (cfg.WriteToken != "" && token == cfg.WriteToken) ||
			(cfg.AdminToken != "" && token == cfg.AdminToken)
	case roleRead:
		return (cfg.ReadToken != "" && token == cfg.ReadToken) ||
			(cfg.WriteToken != "" && token == cfg.WriteToken) ||
			(cfg.AdminToken != "" && token == cfg.AdminToken)
	}
	return false
}
