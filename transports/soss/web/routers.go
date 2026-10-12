// Package web 组装 soss 的 HTTP 传输层。
package web

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/logic"
	"github.com/favos-me/soss/services/monitor"
	"github.com/favos-me/soss/transports/soss/web/controllers"
	sossmw "github.com/favos-me/soss/transports/soss/web/middleware"
)

// Deps 传输层所需依赖，由 main 注入（SKILL.md §10：一个项目一个工厂）。
type Deps struct {
	Config  *config.Config
	Logger  *zap.Logger
	Metrics *monitor.Metrics
}

// NewRouters 构建 Echo 实例与全部路由。
func NewRouters(d Deps) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = controllers.ErrorHandler(d.Logger)
	controllers.SetLogger(d.Logger)

	srv := &d.Config.Server
	e.Server.ReadTimeout = time.Duration(srv.ReadTimeoutSec) * time.Second
	e.Server.WriteTimeout = time.Duration(srv.WriteTimeoutSec) * time.Second
	e.Server.IdleTimeout = time.Duration(srv.IdleTimeoutSec) * time.Second

	e.Pre(middleware.RemoveTrailingSlash())
	e.Use(
		sossmw.RequestID(d.Logger),
		sossmw.RequestLogger(d.Logger, d.Metrics.Incr),
		sossmw.Recover(d.Logger),
	)

	e.GET("/healthz", func(c echo.Context) error {
		readOnly := false
		if logic.Logic != nil {
			readOnly = logic.Logic.ReadOnly()
		}
		return c.JSON(http.StatusOK, map[string]any{"status": "ok", "read_only": readOnly})
	})

	// ---------- 对象 API（design §34 ~ §42）----------
	//
	// 读（GET / HEAD）匿名放行；写（PUT / DELETE）必须携带合法的 Authorization header，
	// 否则 403。桶的增删改查已迁到 /admin，走会话鉴权。
	api := e.Group("/v1")
	api.PUT("/buckets/:bucket/objects/*", controllers.ObjectPutPUT,
		sossmw.RequireWriteToken(&d.Config.Auth, d.Logger))
	api.DELETE("/buckets/:bucket/objects/*", controllers.ObjectDeleteDELETE,
		sossmw.RequireWriteToken(&d.Config.Auth, d.Logger))
	api.GET("/buckets/:bucket/objects/*", controllers.ObjectGetGET)
	api.HEAD("/buckets/:bucket/objects/*", controllers.ObjectHeadHEAD)

	// ---------- 运行态指标（design §87）----------
	if d.Config.Server.MetricsEnabled {
		api.GET("/metrics", controllers.MetricsGET, sossmw.Auth(&d.Config.Auth, d.Logger))
	}

	// ---------- 管理后台 ----------
	if d.Config.Admin.Enabled {
		mountAdmin(e, d)
	}
	return e
}

// mountAdmin 挂载 /admin 的页面路由与 JSON API。
func mountAdmin(e *echo.Echo, d Deps) {
	e.Renderer = newWebRenderer(d.Config.Admin.ViewsDir, d.Logger)
	e.Static("/static", d.Config.Admin.StaticDir)

	admin := e.Group(consts.AdminPathPrefix)
	// 登录/登出本身不能被会话中间件拦住。
	admin.GET("/login", controllers.AdminLoginGET)
	admin.POST("/login", controllers.AdminLoginPOST)
	admin.GET("/logout", controllers.AdminLogoutGET)

	session := sossmw.AdminSession(d.Config, d.Logger)

	// 页面：未登录 302 到登录页。
	page := admin.Group("", session)
	page.GET("", controllers.AdminBucketsGET)
	page.GET("/", controllers.AdminBucketsGET)
	page.GET("/buckets", controllers.AdminBucketsGET)
	page.GET("/buckets/:bucket", controllers.AdminBucketGET)

	// JSON API：未登录 401，供页面上的 fetch 调用。
	adminAPI := admin.Group("/api", session)
	adminAPI.GET("/buckets", controllers.AdminBucketListGET)
	adminAPI.POST("/buckets", controllers.AdminBucketCreatePOST)
	adminAPI.GET("/buckets/:bucket", controllers.AdminBucketDetailGET)
	adminAPI.PATCH("/buckets/:bucket", controllers.AdminBucketUpdatePATCH)
	adminAPI.DELETE("/buckets/:bucket", controllers.AdminBucketDeleteDELETE)
	adminAPI.GET("/buckets/:bucket/objects", controllers.AdminObjectListGET)
	adminAPI.POST("/buckets/:bucket/objects/*", controllers.AdminObjectUploadPOST)
	adminAPI.GET("/buckets/:bucket/objects/*", controllers.AdminObjectMetaGET)
	adminAPI.DELETE("/buckets/:bucket/objects/*", controllers.AdminObjectDeleteDELETE)
}
