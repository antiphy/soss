package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/favos-me/soss/config"
	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/utils"
)

// TestRequireWriteToken 对象写操作的 header 校验：
// 命中 write/admin token 放行，缺失或错误一律 403。
func TestRequireWriteToken(t *testing.T) {
	writeToken := "wt-" + utils.NewXID()
	adminToken := "at-" + utils.NewXID()

	cases := []struct {
		name   string
		cfg    *config.AuthConfig
		header string
		want   int
	}{
		{"write token accepted", &config.AuthConfig{WriteToken: writeToken}, "Bearer " + writeToken, http.StatusOK},
		{"admin token accepted", &config.AuthConfig{AdminToken: adminToken}, "Bearer " + adminToken, http.StatusOK},
		{"raw token without bearer", &config.AuthConfig{WriteToken: writeToken}, writeToken, http.StatusOK},
		{"missing header", &config.AuthConfig{WriteToken: writeToken}, "", http.StatusForbidden},
		{"wrong token", &config.AuthConfig{WriteToken: writeToken}, "Bearer nope", http.StatusForbidden},
		{"token of another role", &config.AuthConfig{WriteToken: writeToken}, "Bearer " + adminToken, http.StatusForbidden},
		{"no token configured", &config.AuthConfig{}, "Bearer anything", http.StatusForbidden},
		{"no token configured, no header", &config.AuthConfig{}, "", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPut, "/v1/buckets/b/objects/k", nil)
			if tc.header != "" {
				req.Header.Set(consts.HeaderAuthorization, tc.header)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			reached := false
			err := RequireWriteToken(tc.cfg, zap.NewNop())(func(c echo.Context) error {
				reached = true
				return c.NoContent(http.StatusOK)
			})(c)
			if err != nil {
				t.Fatalf("middleware returned error: %v", err)
			}
			if rec.Code != tc.want {
				t.Fatalf("got status %d, want %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
			if (tc.want == http.StatusOK) != reached {
				t.Fatalf("handler reached=%v, want %v", reached, tc.want == http.StatusOK)
			}
			if tc.want == http.StatusForbidden {
				if ct := rec.Header().Get(echo.HeaderContentType); ct == "" {
					t.Fatal("403 response should carry a content type")
				}
				if body := rec.Body.String(); body == "" ||
					!contains(body, "PERMISSION_DENIED") {
					t.Fatalf("403 body should report PERMISSION_DENIED, got %q", body)
				}
			}
		})
	}
}

func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
