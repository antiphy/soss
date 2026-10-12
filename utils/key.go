package utils

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ValidateObjectKey 校验 object key（设计文档 §82）。
//
// 规则：
//  1. 非空且不超过 MaxObjectKeyBytes 个字节；
//  2. 必须是合法 UTF-8；
//  3. 不以 "/" 开头或结尾，不包含 "\\"、NUL 与控制字符；
//  4. 不允许包含 ".." 段，防止路径穿越；
//  5. object key 只是逻辑 key，永远不映射为文件系统路径。
func ValidateObjectKey(key string, maxBytes int) error {
	if key == "" {
		return errors.New("object key is empty")
	}
	if len(key) > maxBytes {
		return fmt.Errorf("object key exceeds %d bytes", maxBytes)
	}
	if !utf8.ValidString(key) {
		return errors.New("object key is not valid utf-8")
	}
	if strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") {
		return errors.New("object key must not start or end with '/'")
	}
	if strings.Contains(key, "\\") || strings.ContainsRune(key, 0) {
		return errors.New("object key contains illegal characters")
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return errors.New("object key contains control characters")
		}
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == ".." || strings.Contains(seg, "..") {
			return errors.New("object key must not contain '..'")
		}
		if seg == "" {
			return errors.New("object key contains empty segment")
		}
	}
	return nil
}

// ValidateBucketName 校验 bucket 名称：3~63 个字符，仅小写字母、数字、'-'、'_'、'.'。
//
// 字符集与 readme / 后台页面的桶名提示保持一致，改动任意一侧都要同步另一侧。
func ValidateBucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return errors.New("bucket name length must be between 3 and 63")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.':
		default:
			return errors.New("bucket name may only contain [a-z0-9-_.]")
		}
	}
	if !isLetterOrDigit(name[0]) {
		return errors.New("bucket name must start with a letter or digit")
	}
	return nil
}

// isLetterOrDigit 判断字节是否为小写字母或数字。
func isLetterOrDigit(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}
