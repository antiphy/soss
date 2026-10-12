package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/utils"
)

func testAdminConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	// 测试用随机密钥，避免把任何固定密钥写进仓库。
	cfg.Auth.JWTSecret = utils.NewXID() + utils.NewXID()
	return cfg
}

// TestVerifyAdmin 账号表本身要能验证通过，且错密码/未知用户必须拒绝。
func TestVerifyAdmin(t *testing.T) {
	if len(adminAccounts) == 0 {
		t.Fatal("adminAccounts must not be empty")
	}
	for user, pass := range adminAccounts {
		if !VerifyAdmin(user, pass) {
			t.Fatalf("VerifyAdmin(%q) should succeed", user)
		}
		if VerifyAdmin(user, pass+"x") {
			t.Fatalf("VerifyAdmin(%q) must reject wrong password", user)
		}
	}
	if VerifyAdmin("no-such-user", "whatever") {
		t.Fatal("unknown user must be rejected")
	}
	if VerifyAdmin("", "") {
		t.Fatal("empty credentials must be rejected")
	}
}

// TestAdminTokenRoundTrip 签发 -> 解析，以及过期/被篡改的令牌必须失效。
func TestAdminTokenRoundTrip(t *testing.T) {
	secret := utils.NewXID()
	token, err := SignAdminToken(secret, "tester", time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	user, err := ParseAdminToken(secret, token)
	if err != nil || user != "tester" {
		t.Fatalf("parse got (%q, %v), want (tester, nil)", user, err)
	}
	if _, err := ParseAdminToken(secret+"x", token); err == nil {
		t.Fatal("token signed by another secret must be rejected")
	}
	if _, err := ParseAdminToken(secret, token+"x"); err == nil {
		t.Fatal("tampered token must be rejected")
	}
	expired, err := SignAdminToken(secret, "tester", -time.Hour)
	if err != nil {
		t.Fatalf("sign expired: %v", err)
	}
	if _, err := ParseAdminToken(secret, expired); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

// TestAdminSessionUnauthenticated 未登录：页面 302 到登录页，JSON API 401。
func TestAdminSessionUnauthenticated(t *testing.T) {
	cfg := testAdminConfig(t)
	cases := []struct {
		path string
		want int
	}{
		{"/admin/buckets", http.StatusFound},
		{"/admin/api/buckets", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		handler := AdminSession(cfg, zap.NewNop())(func(c echo.Context) error {
			t.Fatal("handler must not be reached when unauthenticated")
			return nil
		})
		if err := handler(c); err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if rec.Code != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.path, rec.Code, tc.want)
		}
		if tc.want == http.StatusFound && rec.Header().Get(echo.HeaderLocation) != consts.AdminLoginPath {
			t.Fatalf("redirect to %q, want %q", rec.Header().Get(echo.HeaderLocation), consts.AdminLoginPath)
		}
	}
}

// TestAdminSessionAuthenticated 登录后放行并把用户名注入 echo.Context。
func TestAdminSessionAuthenticated(t *testing.T) {
	cfg := testAdminConfig(t)
	token, err := SignAdminToken(cfg.Auth.JWTSecret, "tester", time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/buckets", nil)
	req.AddCookie(&http.Cookie{Name: consts.AdminCookieName, Value: token})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var gotUser string
	handler := AdminSession(cfg, zap.NewNop())(func(c echo.Context) error {
		gotUser = AdminUser(c)
		return c.String(http.StatusOK, "ok")
	})
	if err := handler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if gotUser != "tester" {
		t.Fatalf("got user %q, want tester", gotUser)
	}
}

// TestIssueAndRevokeSession 登录成功后 Cookie 要能被后续请求带上，登出后失效。
func TestIssueAndRevokeSession(t *testing.T) {
	cfg := testAdminConfig(t)

	// 下发会话。
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/admin/login", nil), rec)
	if err := IssueAdminSession(c, cfg, "tester"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	cookies := responseCookies(t, rec)
	if len(cookies) != 1 || cookies[0].Name != consts.AdminCookieName {
		t.Fatalf("session cookie not set: %v", cookies)
	}
	issued := cookies[0]
	if !issued.HttpOnly || issued.Path != consts.AdminCookiePath || issued.Value == "" {
		t.Fatalf("cookie missing attributes: %+v", issued)
	}

	// 带着该 Cookie 访问应放行。
	req := httptest.NewRequest(http.MethodGet, "/admin/buckets", nil)
	req.AddCookie(issued)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req, rec2)
	reached := false
	if err := AdminSession(cfg, zap.NewNop())(func(c echo.Context) error {
		reached = true
		return nil
	})(c2); err != nil {
		t.Fatalf("session rejected valid cookie: %v", err)
	}
	if !reached {
		t.Fatal("handler not reached with a valid session cookie")
	}

	// 登出后 Cookie 被清空（MaxAge < 0 且值为空）。
	rec3 := httptest.NewRecorder()
	c3 := e.NewContext(httptest.NewRequest(http.MethodGet, "/admin/logout", nil), rec3)
	RevokeAdminSession(c3)
	cleared := responseCookies(t, rec3)
	if len(cleared) != 1 || cleared[0].Value != "" || cleared[0].MaxAge >= 0 {
		t.Fatalf("logout cookie not cleared: %+v", cleared)
	}
}

// TestAdminSessionMiddlewareIsSkippable 登录页本身不能被会话中间件拦住（路由层面的保证）。
func TestAdminSessionMiddlewareIsSkippable(t *testing.T) {
	if !isAdminAPI("/admin/api/buckets") || !isAdminAPI("/admin/api") {
		t.Fatal("admin api path detection broken")
	}
	if isAdminAPI("/admin/buckets") || isAdminAPI("/admin/login") {
		t.Fatal("page paths must not be treated as api")
	}
}

// ---------- 辅助 ----------

// responseCookies 从 recorder 的 Set-Cookie 头里解析出 Cookie 列表。
func responseCookies(t *testing.T, rec *httptest.ResponseRecorder) []*http.Cookie {
	t.Helper()
	resp := &http.Response{Header: rec.Header()}
	cookies := resp.Cookies()
	return cookies
}
