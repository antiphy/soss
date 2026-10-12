package web

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	// 注册 pongo2 自定义过滤器（datetime / size ...）。
	_ "github.com/favos-me/soss/transports/soss/web/viewfuncs"
)

// webTemplateRenderer 用 pongo2 预编译模板并实现 echo.Renderer。
//
// 与 meta 项目同样的做法：启动时把模板目录下的 .html 全部编译进 map，
// 请求期只做 ExecuteWriter。区别是这里用 RWMutex 保护 map，避免并发读写 panic。
type webTemplateRenderer struct {
	viewpath string
	logger   *zap.Logger

	mu        sync.RWMutex
	templates map[string]*pongo2.Template
}

// Render 实现 echo.Renderer。
func (t *webTemplateRenderer) Render(w io.Writer, name string, data any, c echo.Context) error {
	dataMap, ok := data.(map[string]any)
	if !ok {
		if data == nil {
			dataMap = map[string]any{}
		} else {
			return errors.New("template data must be map[string]any")
		}
	}
	t.mu.RLock()
	tpl, exist := t.templates[name]
	t.mu.RUnlock()
	if !exist {
		return errors.New("template " + name + " not found")
	}
	return tpl.ExecuteWriter(dataMap, w)
}

// Count 返回已编译模板数量，0 表示模板目录缺失。
func (t *webTemplateRenderer) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.templates)
}

// ViewPath 返回模板根目录。
func (t *webTemplateRenderer) ViewPath() string { return t.viewpath }

// newWebRenderer 编译 dir 下的全部 .html 模板，key 为相对 dir 的斜杠路径。
//
// 模板目录不存在时不 panic：只记录 ERROR 并返回空渲染器，
// 这样管理后台挂掉也不会拖垮对象存储主服务。
func newWebRenderer(dir string, logger *zap.Logger) *webTemplateRenderer {
	r := &webTemplateRenderer{
		viewpath:  dir,
		logger:    logger,
		templates: make(map[string]*pongo2.Template),
	}
	if logger == nil {
		r.logger = zap.NewNop()
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		r.logger.Error("admin template dir unavailable, admin pages will fail",
			zap.String("dir", dir), zap.Error(err))
		return r
	}

	loaded := make(map[string]*pongo2.Template)
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".html") {
			return nil
		}
		tpl, err := pongo2.FromFile(path)
		if err != nil {
			return errors.New("compile template " + path + ": " + err.Error())
		}
		key := strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(dir)+"/")
		loaded[key] = tpl
		return nil
	})
	if walkErr != nil {
		r.logger.Error("load admin templates failed", zap.String("dir", dir), zap.Error(walkErr))
		return r
	}

	r.mu.Lock()
	r.templates = loaded
	r.mu.Unlock()
	r.logger.Info("admin templates loaded", zap.String("dir", dir), zap.Int("count", len(loaded)))
	return r
}
