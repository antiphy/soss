package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/favos-me/soss/config"
)

// testWriteToken 默认配置里 token 为空即拒绝一切写入，测试需显式注入。
const testWriteToken = "key-test-write-token"

// TestObjectKeyPercentNotRedecoded 通配段里的 object key 只能解码一次。
//
// Echo 交给控制器的已是解码后的 key，再 PathUnescape 一次会把字面的 %2F 变成 "/"，
// 于是 v1 上传静默改名，后续 meta/versions/get/delete 全部 404。
func TestObjectKeyPercentNotRedecoded(t *testing.T) {
	e, cfg, cookie := bootTestApp(t, func(cfg *config.Config) { cfg.Auth.WriteToken = testWriteToken })
	write := map[string]string{
		echo.HeaderAuthorization: "Bearer " + cfg.Auth.WriteToken,
		echo.HeaderContentType:   echo.MIMETextPlainCharsetUTF8,
	}

	if rec := do(t, e, cookie, http.MethodPost, "/admin/api/buckets", `{"name":"pct","class":2}`); rec.Code != http.StatusCreated {
		t.Fatalf("create bucket: got %d body=%s", rec.Code, rec.Body.String())
	}

	// key 的字面值是 a%2Fb.txt，URL 里必须写成 a%252Fb.txt。
	const v1Target = "/v1/buckets/pct/objects/a%252Fb.txt"
	const wantKey = "a%2Fb.txt"

	rec := raw(t, e, http.MethodPut, v1Target, "hello", write)
	if rec.Code != http.StatusOK {
		t.Fatalf("put object: got %d body=%s", rec.Code, rec.Body.String())
	}
	var put struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &put); err != nil {
		t.Fatalf("decode put resp: %v body=%s", err, rec.Body.String())
	}
	if put.ObjectKey != wantKey {
		t.Fatalf("put renamed the key: got %q, want %q", put.ObjectKey, wantKey)
	}

	steps := []struct {
		name   string
		method string
		target string
		admin  bool
	}{
		{"v1 get", http.MethodGet, v1Target, false},
		// v1 的版本列表走 key 末尾的 /versions 后缀（不是 query），同样要按一次解码还原 key。
		{"v1 versions", http.MethodGet, v1Target + "/versions", false},
		{"admin meta", http.MethodGet, "/admin/api/buckets/pct/objects/a%252Fb.txt", true},
		{"v1 delete", http.MethodDelete, v1Target, false},
	}
	for _, st := range steps {
		var got *httptest.ResponseRecorder
		if st.admin {
			got = do(t, e, cookie, st.method, st.target, "")
		} else {
			got = raw(t, e, st.method, st.target, "", write)
		}
		if got.Code != http.StatusOK {
			t.Fatalf("%s: got %d body=%s", st.name, got.Code, got.Body.String())
		}
		if st.name == "v1 versions" && !strings.Contains(got.Body.String(), wantKey) {
			t.Fatalf("%s resolved wrong key: %s", st.name, got.Body.String())
		}
	}

	if got := raw(t, e, http.MethodGet, v1Target, "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("get after delete: got %d, want 404", got.Code)
	}
}

// TestObjectKeyWithSlashStillWorks 多段 key（含空格）的读写不因去掉二次解码而回退。
func TestObjectKeyWithSlashStillWorks(t *testing.T) {
	e, cfg, cookie := bootTestApp(t, func(cfg *config.Config) { cfg.Auth.WriteToken = testWriteToken })
	write := map[string]string{
		echo.HeaderAuthorization: "Bearer " + cfg.Auth.WriteToken,
		echo.HeaderContentType:   echo.MIMETextPlainCharsetUTF8,
	}
	if rec := do(t, e, cookie, http.MethodPost, "/admin/api/buckets", `{"name":"plain","class":2}`); rec.Code != http.StatusCreated {
		t.Fatalf("create bucket: got %d", rec.Code)
	}

	// httptest.NewRequest 不接受裸空格，URL 里一律写成 %20。
	target := "/v1/buckets/plain/objects/2024/01/a%20b.txt"
	if rec := raw(t, e, http.MethodPut, target, "body", write); rec.Code != http.StatusOK {
		t.Fatalf("put: got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := raw(t, e, http.MethodGet, target, "", nil); rec.Code != http.StatusOK || rec.Body.String() != "body" {
		t.Fatalf("get: got %d body=%q", rec.Code, rec.Body.String())
	}
	got := do(t, e, cookie, http.MethodGet, "/admin/api/buckets/plain/objects/2024/01/a%20b.txt", "")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"object_key":"2024/01/a b.txt"`) {
		t.Fatalf("admin meta: got %d body=%s", got.Code, got.Body.String())
	}
}
