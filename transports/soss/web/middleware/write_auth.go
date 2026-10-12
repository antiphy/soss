package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/errors"
)

// RequireWriteToken 对象写操作（PUT / DELETE）的 header 校验中间件。
//
// 规则很简单：请求的 Authorization header 必须命中配置的 write_token 或 admin_token，
// 否则一律 403（不是 401 —— 未携带合法凭据视为"不允许"，而不是"未认证"）。
//
// 与 Auth 中间件不同，这里**不做开发模式放行**：写操作默认就是拒绝的，
// 必须在配置里显式给出 token 才能写入。
func RequireWriteToken(cfg *config.AuthConfig, logger *zap.Logger) echo.MiddlewareFunc {
	configured := cfg.WriteToken != "" || cfg.AdminToken != ""
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if configured {
				if token := bearerToken(c.Request().Header.Get(consts.HeaderAuthorization)); token != "" &&
					((cfg.WriteToken != "" && token == cfg.WriteToken) ||
						(cfg.AdminToken != "" && token == cfg.AdminToken)) {
					return next(c)
				}
			}
			Logger(c, logger).Warn("object write rejected: missing or invalid authorization header",
				zap.String("method", c.Request().Method),
				zap.String("path", c.Request().URL.Path),
				zap.Bool("token_configured", configured),
			)
			return c.JSON(http.StatusForbidden, map[string]string{
				"code":    errors.CodeName(errors.CodePermissionDenied),
				"message": errors.Message(errors.CodePermissionDenied),
			})
		}
	}
}
