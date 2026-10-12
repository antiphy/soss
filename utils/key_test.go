package utils

import (
	"strings"
	"testing"
)

// 桶名字符集与 readme / 后台页面的提示是同一条约定，改动任意一侧要同步另一侧。
func TestValidateBucketName(t *testing.T) {
	ok := []string{"abc", "photos", "a-b", "a_b", "my.bucket", "log.2024-01_v1", "abc123"}
	for _, name := range ok {
		if err := ValidateBucketName(name); err != nil {
			t.Errorf("ValidateBucketName(%q): unexpected error %v", name, err)
		}
	}

	bad := []string{"", "ab", "-abc", "_abc", ".abc", "ABC", "ab c", "ab/c", "ab%cc",
		strings.Repeat("x", 64)}
	for _, name := range bad {
		if err := ValidateBucketName(name); err == nil {
			t.Errorf("ValidateBucketName(%q): expected error, got nil", name)
		}
	}
}
