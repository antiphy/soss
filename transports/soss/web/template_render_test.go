package web

import (
	"bytes"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/consts/types"
	"github.com/favos-me/soss/models"
)

// testViewsDir 相对本包目录的模板根：transports/soss/web -> <repo>/views。
const testViewsDir = "../../../views"

func newTestRenderer(t *testing.T) *webTemplateRenderer {
	t.Helper()
	r := newWebRenderer(testViewsDir, zap.NewNop())
	if r.Count() == 0 {
		t.Fatalf("no template compiled from %q (does the dir exist?)", testViewsDir)
	}
	return r
}

// TestAdminTemplatesRender 逐个渲染管理后台模板，捕捉 pongo2 语法/变量错误。
//
// 模板只在运行时解析，语法写错编译期发现不了，所以必须有一条渲染测试兜底。
func TestAdminTemplatesRender(t *testing.T) {
	r := newTestRenderer(t)

	buckets := []models.BucketView{
		{Bucket: models.Bucket{ID: 1, Name: "photos", Class: types.BucketClassHot,
			CreateTS: 1700000000, UpdateTS: 1700000100}, ObjectCount: 3, TotalBytes: 4096},
		{Bucket: models.Bucket{ID: 2, Name: "logs", Class: types.BucketClassNormal,
			CreateTS: 1700000200, UpdateTS: 0}, ObjectCount: 0, TotalBytes: 0},
	}
	objects := &models.ObjectListRes{
		Bucket: "photos",
		Prefix: "2024/",
		Items: []models.ObjectItem{
			{ObjectKey: "2024/a.jpg", Version: 1700000001, Size: 2048,
				ContentType: "image/jpeg", CreatedAt: 1700000001},
			{ObjectKey: "pct/100%.txt", Version: 1700000002, Size: 1024,
				ContentType: "text/plain", CreatedAt: 1700000002},
		},
		Total: 2,
	}

	cases := []struct {
		name string
		tpl  string
		data map[string]any
		want []string
	}{
		{
			name: "login",
			tpl:  "login.html",
			data: map[string]any{"title": "登录", "version": consts.AppVersion, "error": ""},
			want: []string{"SOSS 管理后台", `action="/admin/login"`},
		},
		{
			name: "login with error",
			tpl:  "login.html",
			data: map[string]any{"title": "登录", "version": consts.AppVersion, "error": "用户名或密码错误"},
			want: []string{"用户名或密码错误", "flash error"},
		},
		{
			name: "buckets",
			tpl:  "buckets.html",
			data: map[string]any{"title": "存储桶", "buckets": buckets, "user": "admin",
				"version": consts.AppVersion, "read_only": false},
			want: []string{"photos", "logs",
				`<span class="badge badge-hot">HOT</span>`,
				`<span class="badge badge-normal">NORMAL</span>`,
				"4.00KiB", "/admin/buckets/photos", "admin"},
		},
		{
			name: "bucket detail",
			tpl:  "bucket.html",
			data: map[string]any{"title": "photos", "bucket": buckets[0], "objects": objects,
				"user": "admin", "version": consts.AppVersion, "read_only": true},
			want: []string{"photos", "2024/a.jpg", "<b>HOT</b>", "只读模式", "/static/admin.js", "id=\"modal\"",
				// key 里的 "%" 必须转义，否则下载链接指向另一个对象。
				"objects/pct/100%25.txt",
				// prefix 是 query 参数，整段都要转义。
				"?prefix=pct%2F100%25.txt"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := r.Render(&buf, consts.DefaultViewLang+"/admin/"+tc.tpl, tc.data, nil); err != nil {
				t.Fatalf("render %s: %v", tc.tpl, err)
			}
			out := buf.String()
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Fatalf("render %s: output missing %q\n--- output ---\n%s", tc.tpl, w, out)
				}
			}
		})
	}
}

// TestAdminRendererMissingDir 模板目录不存在时不能 panic，只记录错误并返回空渲染器。
func TestAdminRendererMissingDir(t *testing.T) {
	r := newWebRenderer("./definitely-not-exist", zap.NewNop())
	if r.Count() != 0 {
		t.Fatalf("expected empty renderer, got %d templates", r.Count())
	}
}

// TestAdminScriptOrder admin.js 依赖 window.SOSS_ADMIN 决定挂哪个页面的监听，
// 赋值脚本必须排在它前面；顺序反了所有按钮和表单都会静默失效（表单退化成原生 GET）。
func TestAdminScriptOrder(t *testing.T) {
	r := newTestRenderer(t)

	cases := []struct {
		tpl  string
		data map[string]any
	}{
		{"buckets.html", map[string]any{"title": "存储桶", "buckets": []models.BucketView{}}},
		{"bucket.html", map[string]any{"title": "photos",
			"bucket":  &models.BucketView{Bucket: models.Bucket{Name: "photos", Class: types.BucketClassNormal}},
			"objects": &models.ObjectListRes{Bucket: "photos"}}},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		if err := r.Render(&buf, consts.DefaultViewLang+"/admin/"+tc.tpl, tc.data, nil); err != nil {
			t.Fatalf("render %s: %v", tc.tpl, err)
		}
		out := buf.String()
		cfg := strings.Index(out, "window.SOSS_ADMIN")
		js := strings.Index(out, `src="/static/admin.js"`)
		if cfg < 0 || js < 0 {
			t.Fatalf("render %s: missing SOSS_ADMIN(%d) or admin.js(%d)", tc.tpl, cfg, js)
		}
		if cfg > js {
			t.Errorf("render %s: window.SOSS_ADMIN assigned after admin.js loads (cfg=%d js=%d)", tc.tpl, cfg, js)
		}
	}
}
