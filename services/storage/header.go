package storage

import (
	"encoding/binary"
	"errors"

	"github.com/favos-me/soss/utils"
)

// 记录头格式（design §8）：
//
//	Magic           8 bytes
//	FormatVersion   2 bytes
//	HeaderSize      2 bytes
//	ObjectVersion   8 bytes
//	PayloadSize     8 bytes
//	CRC32C          4 bytes
//	Flags           4 bytes
//	Reserved        28 bytes
//	--------------------------------
//	Total           64 bytes
const (
	// HeaderSize 记录头固定大小。
	HeaderSize = 64
	// FormatVersion 当前记录格式版本。
	FormatVersion = 1
	// VersionFieldOffset ObjectVersion 字段在 header 内的偏移，用于版本回填。
	VersionFieldOffset = 16

	// flagHasFilename 预留：payload 之后携带 filename 扩展信息。
	flagHasFilename uint32 = 1 << 0
)

// Magic 记录头魔数，固定 8 字节。
var Magic = [8]byte{'S', 'O', 'S', 'S', 'R', 'E', 'C', '1'}

// ErrBadMagic 记录头魔数不匹配。
var ErrBadMagic = errors.New("bad record magic")

// ErrBadHeaderSize 记录头大小不匹配。
var ErrBadHeaderSize = errors.New("bad record header size")

// ErrBadPayloadSize 记录头记录的 payload 大小与元数据不一致。
var ErrBadPayloadSize = errors.New("bad record payload size")

// ErrChecksumMismatch CRC32C 校验失败。
var ErrChecksumMismatch = errors.New("record checksum mismatch")

// RecordHeader 解码后的记录头。
type RecordHeader struct {
	FormatVersion uint16
	HeaderSize    uint16
	ObjectVersion int64
	PayloadSize   int64
	CRC32C        uint32
	Flags         uint32
}

// EncodeHeader 序列化记录头。
func EncodeHeader(h RecordHeader) [HeaderSize]byte {
	var buf [HeaderSize]byte
	copy(buf[0:8], Magic[:])
	binary.LittleEndian.PutUint16(buf[8:10], h.FormatVersion)
	binary.LittleEndian.PutUint16(buf[10:12], HeaderSize)
	binary.LittleEndian.PutUint64(buf[12:20], uint64(h.ObjectVersion))
	binary.LittleEndian.PutUint64(buf[20:28], uint64(h.PayloadSize))
	binary.LittleEndian.PutUint32(buf[28:32], h.CRC32C)
	binary.LittleEndian.PutUint32(buf[32:36], h.Flags)
	// buf[36:64] 为 Reserved，保持 0。
	return buf
}

// DecodeHeader 反序列化记录头并做基本校验。
func DecodeHeader(buf []byte) (RecordHeader, error) {
	if len(buf) < HeaderSize {
		return RecordHeader{}, ErrBadHeaderSize
	}
	for i := 0; i < len(Magic); i++ {
		if buf[i] != Magic[i] {
			return RecordHeader{}, ErrBadMagic
		}
	}
	h := RecordHeader{
		FormatVersion: binary.LittleEndian.Uint16(buf[8:10]),
		HeaderSize:    binary.LittleEndian.Uint16(buf[10:12]),
		ObjectVersion: int64(binary.LittleEndian.Uint64(buf[12:20])),
		PayloadSize:   int64(binary.LittleEndian.Uint64(buf[20:28])),
		CRC32C:        binary.LittleEndian.Uint32(buf[28:32]),
		Flags:         binary.LittleEndian.Uint32(buf[32:36]),
	}
	if h.HeaderSize != HeaderSize {
		return RecordHeader{}, ErrBadHeaderSize
	}
	return h, nil
}

// EncodeVersionField 只序列化 ObjectVersion 字段，用于版本回填。
func EncodeVersionField(version int64) [8]byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(version))
	return buf
}

// NewHeader 构造一个记录头。
func NewHeader(version int64, payloadSize int64, flags uint32) RecordHeader {
	return RecordHeader{
		FormatVersion: FormatVersion,
		HeaderSize:    HeaderSize,
		ObjectVersion: version,
		PayloadSize:   payloadSize,
		Flags:         flags,
	}
}

// Checksum 计算 payload 的 CRC32C。
func Checksum(payload []byte) uint32 {
	return utils.CRC32CChecksum(payload)
}
