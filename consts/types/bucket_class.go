package types

import (
	"encoding/json"
	"strings"
)

// BucketClass 存储桶类型（设计文档 §19）。
//
//	1 = HOT    全量内存驻留
//	2 = NORMAL LRU 缓存
type BucketClass int

const (
	BucketClassHot    BucketClass = 1
	BucketClassNormal BucketClass = 2
)

// String 返回可读名称。
func (c BucketClass) String() string {
	switch c {
	case BucketClassHot:
		return "HOT"
	case BucketClassNormal:
		return "NORMAL"
	default:
		return "UNKNOWN"
	}
}

// Valid 判断是否为合法取值。
func (c BucketClass) Valid() bool {
	return c == BucketClassHot || c == BucketClassNormal
}

// MarshalJSON 输出数字，保持 API 与文档一致。
func (c BucketClass) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(c))
}

// UnmarshalJSON 同时接受 1/2 与 "HOT"/"NORMAL"。
func (c *BucketClass) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if raw == "" || raw == "null" {
		*c = 0
		return nil
	}
	switch strings.ToUpper(raw) {
	case "HOT", "1":
		*c = BucketClassHot
		return nil
	case "NORMAL", "2":
		*c = BucketClassNormal
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*c = BucketClass(n)
	return nil
}
