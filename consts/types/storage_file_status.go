package types

// StorageFileStatus 数据文件状态（设计文档 §76，为后续 segment 化预留）。
type StorageFileStatus int

const (
	// StorageFileStatusActive 当前可写入的数据文件。
	StorageFileStatusActive StorageFileStatus = 1
	// StorageFileStatusSealed 已封存，只可读不可写（未来 Compact 使用）。
	StorageFileStatusSealed StorageFileStatus = 2
)

// String 返回可读名称。
func (s StorageFileStatus) String() string {
	switch s {
	case StorageFileStatusActive:
		return "ACTIVE"
	case StorageFileStatusSealed:
		return "SEALED"
	default:
		return "UNKNOWN"
	}
}
