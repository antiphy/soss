package sqlite

import (
	"context"
	"database/sql"

	"github.com/favos-me/soss/consts/types"
	"github.com/favos-me/soss/models"
)

// EnsureStorageFile 登记数据文件（v1 只有 data.bin，design §76）。
func (s *Storage) EnsureStorageFile(ctx context.Context, filename string, capacity int64, now int64) (models.StorageFile, error) {
	var sf models.StorageFile
	err := s.writeTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO storage_files (filename, capacity_bytes, write_offset, created_at, status)
			 VALUES (?, ?, 0, ?, ?)`, filename, capacity, now, int(types.StorageFileStatusActive)); err != nil {
			return err
		}
		var status int
		if err := tx.QueryRowContext(ctx,
			`SELECT id, filename, capacity_bytes, write_offset, created_at, status
			 FROM storage_files WHERE filename = ?`, filename,
		).Scan(&sf.ID, &sf.Filename, &sf.CapacityBytes, &sf.WriteOffset, &sf.CreateTS, &status); err != nil {
			return err
		}
		sf.Status = types.StorageFileStatus(status)
		return nil
	})
	if err != nil {
		return models.StorageFile{}, s.finish(err)
	}
	return sf, nil
}

// UpdateStorageFileCapacity 扩容后同步登记容量与写游标。
func (s *Storage) UpdateStorageFileCapacity(ctx context.Context, filename string, capacity, writeOffset int64) error {
	err := s.writeTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE storage_files
			 SET capacity_bytes = MAX(capacity_bytes, ?), write_offset = MAX(write_offset, ?)
			 WHERE filename = ?`, capacity, writeOffset, filename)
		return err
	})
	return s.finish(err)
}
