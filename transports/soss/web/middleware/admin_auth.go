package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/errors"
)

// ---------------------------------------------------------------------------
// 账号系统
//
// 这是一个"够用就好"的最小账号系统：账号表直接硬编码在下面的常量里，
// 不落库、不支持注册/改密。适用场景是本地或内网部署的单机管理后台。
// 若要开放到公网，请改成配置化 + 密码哈希 + 登录失败限流。
// ---------------------------------------------------------------------------

// adminAccounts 硬编码的管理员账号：用户名 -> 明文密码。
var adminAccounts = map[string]string{
	"admin": "soss-admin",
}

// VerifyAdmin 校验管理员账号密码；密码比较用常量时间，避免时序侧信道。
func VerifyAdmin(username, password string) bool {
	want, ok := adminAccounts[username]
	if !ok || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(password)) == 1
}

// ---------- JWT ----------

// adminClaim 管理后台会话的 JWT 载荷。
type adminClaim struct {
	// User 登录用户名。
	User string `json:"user"`
	jwt.RegisteredClaims
}

// SignAdminToken 签发管理员会话令牌。
func SignAdminToken(secret, user string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := adminClaim{
		User: user,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user,
			Issuer:    consts.AppName,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseAdminToken 校验令牌并返回用户名；任何异常（过期/签名不符/伪造算法）都返回 error。
func ParseAdminToken(secret, tokenStr string) (string, error) {
	if tokenStr == "" {
		return "", errors.NewError(errors.CodeUnauthorized)
	}
	claims := &adminClaim{}
	parsed, err := jwt.ParseWithClaims(tokenStr, claims, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithIssuer(consts.AppName))
	if err != nil || !parsed.Valid {
		return "", errors.NewError(errors.CodeUnauthorized)
	}
	if claims.User == "" {
		claims.User = claims.Subject
	}
	if claims.User == "" {
		return "", errors.NewError(errors.CodeUnauthorized)
	}
	return claims.User, nil
}

// ---------- Cookie ----------

// sessionTTL 返回会话有效期。
func sessionTTL(cfg *config.Config) time.Duration {
	hour := cfg.Auth.SessionHour
	if hour <= 0 {
		hour = consts.DefaultAdminSessionHour
	}
	return time.Duration(hour) * time.Hour
}

// IssueAdminSession 签发令牌并写入会话 Cookie。
func IssueAdminSession(c echo.Context, cfg *config.Config, user string) error {
	token, err := SignAdminToken(cfg.Auth.JWTSecret, user, sessionTTL(cfg))
	if err != nil {
		return err
	}
	c.SetCookie(&http.Cookie{
		Name:     consts.AdminCookieName,
		Value:    token,
		Path:     consts.AdminCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL(cfg).Seconds()),
	})
	return nil
}

// RevokeAdminSession 清除会话 Cookie。
func RevokeAdminSession(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     consts.AdminCookieName,
		Value:    "",
		Path:     consts.AdminCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// AdminUserFromRequest 直接从会话 Cookie 解析当前登录用户，不依赖 AdminSession 中间件
// （登录页需要在未登录状态下判断"是否已经登录过"）。
func AdminUserFromRequest(c echo.Context, cfg *config.Config) string {
	if c == nil || cfg == nil {
		return ""
	}
	user, err := userFromCookie(c, cfg.Auth.JWTSecret)
	if err != nil {
		return ""
	}
	return user
}

// AdminUser 取出当前登录用户名，未登录返回空串。
func AdminUser(c echo.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(consts.ContextKeyAdminUser).(string); ok {
		return v
	}
	return ""
}

// ---------- 中间件 ----------

// AdminSession 校验管理后台会话。
//
// 未登录时：JSON API（/admin/api/...）返回 401，页面请求 302 跳转到登录页。
func AdminSession(cfg *config.Config, logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, err := userFromCookie(c, cfg.Auth.JWTSecret)
			if err != nil {
				if isAdminAPI(c.Request().URL.Path) {
					return c.JSON(http.StatusUnauthorized, map[string]string{
						"code":    errors.CodeName(errors.CodeUnauthorized),
						"message": errors.Message(errors.CodeUnauthorized),
					})
				}
				return c.Redirect(http.StatusFound, consts.AdminLoginPath)
			}
			c.Set(consts.ContextKeyAdminUser, user)
			return next(c)
		}
	}
}

// userFromCookie 从会话 Cookie 中解析用户名。
func userFromCookie(c echo.Context, secret string) (string, error) {
	cookie, err := c.Cookie(consts.AdminCookieName)
	if err != nil || cookie == nil || cookie.Value == "" {
		return "", errors.NewError(errors.CodeUnauthorized)
	}
	return ParseAdminToken(secret, cookie.Value)
}

// isAdminAPI 判断是否为管理后台的 JSON API 路径。
func isAdminAPI(path string) bool {
	return path == consts.AdminAPIPrefix || strings.HasPrefix(path, consts.AdminAPIPrefix+"/")
}
