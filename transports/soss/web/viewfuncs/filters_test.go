package viewfuncs

import (
	"testing"
	"time"

	"github.com/flosch/pongo2/v6"

	"github.com/favos-me/soss/consts/types"
)

// 过滤器统一走 toInt64 取值：具名整数类型（types.BucketClass）取不到就会静默降级，
// 模板里只表现为 UNKNOWN/0，编译期和渲染测试都看不出来，所以逐个数位打靶。
func TestClassNameFilterNamedType(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{types.BucketClassHot, "HOT"},
		{types.BucketClassNormal, "NORMAL"},
		{types.BucketClass(0), "UNKNOWN"},
		{int(types.BucketClassHot), "HOT"},
	}
	for _, tc := range cases {
		got, err := classNameFilter(pongo2.AsValue(tc.in), nil)
		if err != nil {
			t.Fatalf("class_name(%v): %v", tc.in, err)
		}
		if got.String() != tc.want {
			t.Errorf("class_name(%#v): got %q, want %q", tc.in, got.String(), tc.want)
		}
	}
}

func TestDatetimeAndSizeFilters(t *testing.T) {
	ts := time.Date(2026, 3, 5, 10, 20, 30, 0, time.Local).Unix()
	got, err := datetimeFilter(pongo2.AsValue(ts), nil)
	if err != nil {
		t.Fatalf("datetime: %v", err)
	}
	if got.String() != "2026-03-05 10:20:30" {
		t.Errorf("datetime(%d): got %q", ts, got.String())
	}
	if got, _ := datetimeFilter(pongo2.AsValue(int64(0)), nil); got.String() != "-" {
		t.Errorf("datetime(0): got %q, want %q", got.String(), "-")
	}

	cases := []struct {
		in   any
		want string
	}{
		{int64(0), "0B"},
		{int64(11), "11B"},
		{types.BucketClass(4096), "4.00KiB"},
		{int64(-1), "0B"},
	}
	for _, tc := range cases {
		got, err := sizeFilter(pongo2.AsValue(tc.in), nil)
		if err != nil {
			t.Fatalf("size(%v): %v", tc.in, err)
		}
		if got.String() != tc.want {
			t.Errorf("size(%#v): got %q, want %q", tc.in, got.String(), tc.want)
		}
	}
}

// urlpath 用在 <a href> 的路径段上：必须逐段转义并保留 "/"，
// 否则会退化成内置 urlencode 那种把 "/" 也编掉的行为。
func TestURLPathFilter(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"a.txt", "a.txt"},
		{"dir/u.txt", "dir/u.txt"},
		{"a b.txt", "a%20b.txt"},
		{"a%2Fb.txt", "a%252Fb.txt"},
		{"100%.txt", "100%25.txt"},
		{"中文.txt", "%E4%B8%AD%E6%96%87.txt"},
	}
	for _, tc := range cases {
		got, err := urlPathFilter(pongo2.AsValue(tc.in), nil)
		if err != nil {
			t.Fatalf("urlpath(%q): %v", tc.in, err)
		}
		if got.String() != tc.want {
			t.Errorf("urlpath(%q): got %q, want %q", tc.in, got.String(), tc.want)
		}
	}
}
