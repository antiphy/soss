// Package viewfuncs 注册 pongo2 自定义过滤器。
//
// 与 meta 项目一样通过 init() 注册到 pongo2 全局默认 set，
// 由 web 包空导入触发（`_ ".../web/viewfuncs"`）。
package viewfuncs

import (
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/flosch/pongo2/v6"

	"github.com/favos-me/soss/consts/types"
	"github.com/favos-me/soss/utils"
)

// datetimeLayout 模板里展示时间用的格式。
const datetimeLayout = "2006-01-02 15:04:05"

func init() {
	pongo2.RegisterFilter("datetime", datetimeFilter)
	pongo2.RegisterFilter("size", sizeFilter)
	pongo2.RegisterFilter("class_name", classNameFilter)
	pongo2.RegisterFilter("urlpath", urlPathFilter)
}

// urlPathFilter 把 object key 编码成 URL 路径片段：按 "/" 分段后逐段转义，保留分隔符。
//
// 内置的 urlencode 会把 "/" 也编成 %2F，用在路径上不对；模板里的 <a href> 必须走这个。
// 用法：{{ o.ObjectKey | urlpath }}
func urlPathFilter(in, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	key := in.String()
	segs := strings.Split(key, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return pongo2.AsValue(strings.Join(segs, "/")), nil
}

// classNameFilter 把 bucket class 数值渲染成 HOT / NORMAL。
//
// 用法：{{ b.Class | class_name }}
func classNameFilter(in, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	switch types.BucketClass(toInt64(in)) {
	case types.BucketClassHot:
		return pongo2.AsValue("HOT"), nil
	case types.BucketClassNormal:
		return pongo2.AsValue("NORMAL"), nil
	}
	return pongo2.AsValue("UNKNOWN"), nil
}

// datetimeFilter 把 Unix 秒渲染成本地时间字符串；0 或非法输入渲染为 "-"。
//
// 用法：{{ obj.CreatedAt | datetime }}
func datetimeFilter(in, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	ts := toInt64(in)
	if ts <= 0 {
		return pongo2.AsValue("-"), nil
	}
	return pongo2.AsValue(time.Unix(ts, 0).Local().Format(datetimeLayout)), nil
}

// sizeFilter 把字节数渲染成人类可读容量。
//
// 用法：{{ obj.Size | size }}
func sizeFilter(in, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	n := toInt64(in)
	if n < 0 {
		n = 0
	}
	return pongo2.AsValue(utils.FormatSize(n)), nil
}

// toInt64 取任意整数（含 types.BucketClass 这类具名类型）与浮点的整数值。
//
// 具名类型的底层 Kind 仍是 int，直接对 v.Interface() 做 type switch 匹配不上，
// 必须走反射，否则模板里的 HOT/NORMAL 会全部退化成 0。
func toInt64(v *pongo2.Value) int64 {
	if v == nil || v.IsNil() {
		return 0
	}
	rv := reflect.ValueOf(v.Interface())
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return int64(rv.Float())
	}
	return 0
}
