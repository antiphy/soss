package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/dal/sqlite"
	"github.com/favos-me/soss/logic"
	"github.com/favos-me/soss/services/monitor"
	"github.com/favos-me/soss/services/recovery"
	"github.com/favos-me/soss/services/storage"
	sossmw "github.com/favos-me/soss/transports/soss/web/middleware"
)

// 相对本包目录（transports/soss/web）定位仓库根下的资源目录。
const (
	testScriptsDir = "../../../scripts"
	testStaticDir  = "../../../static"
)

// bootTestApp 在临时目录里拉起一整套服务（sqlite + data.bin + logic + 路由），
// 返回 echo 实例、生效配置与一份已登录的会话 Cookie。
//
// 会话直接用 SignAdminToken 签，避免在测试里硬编码账号密码；
// 账号密码本身的校验由 middleware.TestVerifyAdmin 覆盖。
func bootTestApp(t *testing.T, tweak func(*config.Config)) (*echo.Echo, *config.Config, *http.Cookie) {
	t.Helper()
	root := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Storage.Root = root
	cfg.Storage.InitialSize = config.Size(8 << 20)
	cfg.Storage.ExpandStep = config.Size(8 << 20)
	cfg.Storage.LowWatermark = config.Size(1 << 20)
	cfg.Storage.DiskSafetyMargin = config.Size(1 << 20)
	cfg.Cache.HotMaxBytes = config.Size(4 << 20)
	cfg.Cache.LRUMaxBytes = config.Size(2 << 20)
	cfg.Server.MaxObjectSize = config.Size(1 << 20)
	// 模板/静态目录默认相对进程 cwd；测试跑在包目录下，这里显式指向仓库根。
	cfg.Admin.ViewsDir = testViewsDir
	cfg.Admin.StaticDir = testStaticDir
	if tweak != nil {
		tweak(cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	logger := zap.NewNop()
	metrics := monitor.NewMetrics()
	alarms := monitor.NewAlarms(logger, metrics)

	ds, err := sqlite.NewStorage(filepath.Join(root, "oss.db"), cfg.SQLite, logger, metrics, alarms)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = ds.Close() })

	if err := sqlite.NewMigrator(ds.DB(), os.DirFS(testScriptsDir), logger).Init(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store, err := storage.Open(root, cfg.Storage, logger, metrics, alarms)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := recovery.Run(t.Context(), ds, store, cfg, logger, metrics, alarms); err != nil {
		t.Fatalf("recovery: %v", err)
	}

	logic.Init(cfg, logger, ds, store, metrics, alarms)
	logic.Logic.Start(nil)
	t.Cleanup(logic.Logic.Stop)

	e := NewRouters(Deps{Config: cfg, Logger: logger, Metrics: metrics})

	token, err := sossmw.SignAdminToken(cfg.Auth.JWTSecret, "tester", time.Hour)
	if err != nil {
		t.Fatalf("sign admin token: %v", err)
	}
	return e, cfg, &http.Cookie{Name: consts.AdminCookieName, Value: token}
}

// do 发起一次带会话 Cookie 的 JSON 请求。
func do(t *testing.T, e *echo.Echo, cookie *http.Cookie, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// raw 发起一次自定义 header 的请求（用于裸 body 上传）。
func raw(t *testing.T, e *echo.Echo, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestAdminBucketAndObjectFlow 端到端跑一遍管理后台的桶/对象增删改查。
func TestAdminBucketAndObjectFlow(t *testing.T) {
	e, _, cookie := bootTestApp(t, nil)

	// 1) 未登录：页面 302、API 401。
	if rec := do(t, e, nil, http.MethodGet, "/admin/buckets", ""); rec.Code != http.StatusFound {
		t.Fatalf("anonymous page: got %d, want 302", rec.Code)
	}
	if rec := do(t, e, nil, http.MethodGet, "/admin/api/buckets", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous api: got %d, want 401", rec.Code)
	}

	// 2) 登录后页面可渲染。
	if rec := do(t, e, cookie, http.MethodGet, "/admin/buckets", ""); rec.Code != http.StatusOK {
		t.Fatalf("buckets page: got %d body=%s", rec.Code, rec.Body.String())
	}

	// 3) 创建桶。
	rec := do(t, e, cookie, http.MethodPost, "/admin/api/buckets", `{"name":"photos","class":2}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create bucket: got %d body=%s", rec.Code, rec.Body.String())
	}

	// 4) 列表可见。
	rec = do(t, e, cookie, http.MethodGet, "/admin/api/buckets", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "photos") {
		t.Fatalf("list buckets: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 5) 上传对象（裸 body）。
	rec = do(t, e, cookie, http.MethodPost, "/admin/api/buckets/photos/objects/2024/a.txt", "hello soss")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: got %d body=%s", rec.Code, rec.Body.String())
	}
	var put map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &put); err != nil {
		t.Fatalf("decode upload response: %v (%s)", err, rec.Body.String())
	}
	if put["object_key"] != "2024/a.txt" {
		t.Fatalf("upload key: got %v", put["object_key"])
	}

	// 6) 匿名 GET 能读到刚上传的对象（读路径不鉴权）。
	rec = do(t, e, nil, http.MethodGet, "/v1/buckets/photos/objects/2024/a.txt", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "hello soss" {
		t.Fatalf("anonymous get: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 7) 对象列表。
	rec = do(t, e, cookie, http.MethodGet, "/admin/api/buckets/photos/objects", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "2024/a.txt") {
		t.Fatalf("list objects: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 8) 覆盖上传产生新版本。
	if rec = do(t, e, cookie, http.MethodPost, "/admin/api/buckets/photos/objects/2024/a.txt", "hello v2"); rec.Code != http.StatusOK {
		t.Fatalf("re-upload: got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, e, cookie, http.MethodGet, "/admin/api/buckets/photos/objects/2024/a.txt?versions=1", "")
	if !strings.Contains(rec.Body.String(), `"total":2`) {
		t.Fatalf("expected 2 versions, body=%s", rec.Body.String())
	}

	// 9) 修改桶类型。
	rec = do(t, e, cookie, http.MethodPatch, "/admin/api/buckets/photos", `{"class":1}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"class":1`) {
		t.Fatalf("patch bucket: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 10) 桶详情。
	rec = do(t, e, cookie, http.MethodGet, "/admin/api/buckets/photos", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "photos") {
		t.Fatalf("bucket detail: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 11) 删除对象 -> 桶变空。
	rec = do(t, e, cookie, http.MethodDelete, "/admin/api/buckets/photos/objects/2024/a.txt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete object: got %d body=%s", rec.Code, rec.Body.String())
	}

	// 12) 删除桶。
	rec = do(t, e, cookie, http.MethodDelete, "/admin/api/buckets/photos", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete bucket: got %d body=%s", rec.Code, rec.Body.String())
	}

	// 13) 桶已消失。
	if rec = do(t, e, cookie, http.MethodGet, "/admin/api/buckets/photos", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("bucket should be gone: got %d", rec.Code)
	}
}

// TestObjectWriteRequiresHeader 对象写必须带合法的 Authorization header，读必须匿名可达。
func TestObjectWriteRequiresHeader(t *testing.T) {
	const writeToken = "unit-write-token"
	e, _, cookie := bootTestApp(t, func(cfg *config.Config) { cfg.Auth.WriteToken = writeToken })

	if rec := do(t, e, cookie, http.MethodPost, "/admin/api/buckets", `{"name":"docs","class":2}`); rec.Code != http.StatusCreated {
		t.Fatalf("create bucket: %d %s", rec.Code, rec.Body.String())
	}

	// 写：无 header -> 403。
	if rec := raw(t, e, http.MethodPut, "/v1/buckets/docs/objects/x.txt", "hi", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("put without header: got %d, want 403", rec.Code)
	}
	// 写：错误 header -> 403。
	if rec := raw(t, e, http.MethodPut, "/v1/buckets/docs/objects/x.txt", "hi",
		map[string]string{consts.HeaderAuthorization: "Bearer wrong"}); rec.Code != http.StatusForbidden {
		t.Fatalf("put with wrong header: got %d, want 403", rec.Code)
	}
	// 写：正确 header -> 放行。
	rec := raw(t, e, http.MethodPut, "/v1/buckets/docs/objects/x.txt", "hi",
		map[string]string{consts.HeaderAuthorization: "Bearer " + writeToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("put with valid header: got %d body=%s", rec.Code, rec.Body.String())
	}

	// 读：匿名 GET 命中刚写入的对象。
	if rec := raw(t, e, http.MethodGet, "/v1/buckets/docs/objects/x.txt", "", nil); rec.Code != http.StatusOK ||
		rec.Body.String() != "hi" {
		t.Fatalf("anonymous get: code=%d body=%q", rec.Code, rec.Body.String())
	}
	// 读：匿名 GET 不存在的对象 -> 404（而不是被拦成 403）。
	if rec := raw(t, e, http.MethodGet, "/v1/buckets/docs/objects/nope.txt", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("anonymous get missing: got %d, want 404", rec.Code)
	}

	// 删除：无 header -> 403，有 header -> 200。
	if rec := raw(t, e, http.MethodDelete, "/v1/buckets/docs/objects/x.txt", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("delete without header: got %d, want 403", rec.Code)
	}
	if rec := raw(t, e, http.MethodDelete, "/v1/buckets/docs/objects/x.txt", "",
		map[string]string{consts.HeaderAuthorization: "Bearer " + writeToken}); rec.Code != http.StatusOK {
		t.Fatalf("delete with valid header: got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestBucketCRUDMovedToAdmin /v1 下的桶路由必须已经撤掉。
func TestBucketCRUDMovedToAdmin(t *testing.T) {
	e, _, _ := bootTestApp(t, nil)
	for _, target := range []string{"/v1/buckets", "/v1/buckets/x"} {
		if rec := raw(t, e, http.MethodGet, target, "", nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d, want 404", target, rec.Code)
		}
	}
}
