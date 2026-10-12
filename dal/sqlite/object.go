package sqlite

import (
	"context"
	"database/sql"
	"time"

	serr "github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/models"
)

const selectVersionSQL = `SELECT o.id, o.object_key, o.latest_version, v.version, v.data_file,
	v.data_offset, v.data_size, v.record_size, v.filename, v.content_type, v.etag, v.checksum, v.created_at
FROM objects o
JOIN object_versions v ON v.object_id = o.id `

// LookupVersion 查询对象某个版本；version <= 0 表示读取 latest（design §62 / §63）。
func (s *Storage) LookupVersion(ctx context.Context, bucketID int64, bucketName, objectKey string, version int64) (models.VersionView, error) {
	var (
		view  models.VersionView
		query string
		args  []any
	)
	if version > 0 {
		query = selectVersionSQL + `AND v.version = ? WHERE o.bucket_id = ? AND o.object_key = ?`
		args = []any{version, bucketID, objectKey}
	} else {
		query = selectVersionSQL + `AND v.version = o.latest_version WHERE o.bucket_id = ? AND o.object_key = ?`
		args = []any{bucketID, objectKey}
	}
	var filename, contentType, etag, checksum sql.NullString
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&view.ObjectID, &view.ObjectKey, &view.LatestVersion, &view.Version,
		&view.DataFile, &view.DataOffset, &view.DataSize, &view.RecordSize,
		&filename, &contentType, &etag, &checksum,
		&view.CreateTS,
	)
	view.Filename = filename.String
	view.ContentType = contentType.String
	view.ETag = etag.String
	view.Checksum = checksum.String
	if err != nil {
		code := serr.CodeObjectNotFound
		if version > 0 {
			code = serr.CodeVersionNotFound
		}
		return models.VersionView{}, s.notFound(err, code)
	}
	view.BucketID = bucketID
	view.BucketName = bucketName
	return view, nil
}

// ListVersions 列出对象全部版本，按版本号倒序（design §40）。
func (s *Storage) ListVersions(ctx context.Context, bucketID int64, objectKey string) ([]models.VersionMeta, error) {
	var objectID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM objects WHERE bucket_id = ? AND object_key = ?`, bucketID, objectKey).Scan(&objectID)
	if err != nil {
		return nil, s.notFound(err, serr.CodeObjectNotFound)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT version, data_size, etag, created_at FROM object_versions
		 WHERE object_id = ? ORDER BY version DESC`, objectID)
	if err != nil {
		return nil, s.wrapErr(err)
	}
	defer rows.Close()

	out := make([]models.VersionMeta, 0, 8)
	for rows.Next() {
		var m models.VersionMeta
		var etag sql.NullString
		if err := rows.Scan(&m.Version, &m.Size, &etag, &m.CreatedAt); err != nil {
			return nil, s.wrapErr(err)
		}
		m.ETag = etag.String
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, s.wrapErr(err)
	}
	return out, nil
}

// ListLatestVersions 列出桶内全部对象的当前版本，用于 HOT warm-up。
func (s *Storage) ListLatestVersions(ctx context.Context, bucketID int64, bucketName string) ([]models.VersionView, error) {
	rows, err := s.db.QueryContext(ctx, selectVersionSQL+`AND v.version = o.latest_version WHERE o.bucket_id = ?`, bucketID)
	if err != nil {
		return nil, s.wrapErr(err)
	}
	defer rows.Close()

	out := make([]models.VersionView, 0, 64)
	for rows.Next() {
		var (
			view                  models.VersionView
			filename, contentType sql.NullString
			etag, checksum        sql.NullString
		)
		if err := rows.Scan(
			&view.ObjectID, &view.ObjectKey, &view.LatestVersion, &view.Version,
			&view.DataFile, &view.DataOffset, &view.DataSize, &view.RecordSize,
			&filename, &contentType, &etag, &checksum,
			&view.CreateTS,
		); err != nil {
			return nil, s.wrapErr(err)
		}
		view.Filename = filename.String
		view.ContentType = contentType.String
		view.ETag = etag.String
		view.Checksum = checksum.String
		view.BucketID = bucketID
		view.BucketName = bucketName
		out = append(out, view)
	}
	if err := rows.Err(); err != nil {
		return nil, s.wrapErr(err)
	}
	return out, nil
}

// CommitPut 在一个事务内完成对象创建/更新、版本插入、latest_version 推进与
// storage_files.write_offset 推进（design §36 / §58 / §59）。
func (s *Storage) CommitPut(ctx context.Context, in models.CommitPutInput) (models.CommitPutResult, error) {
	var result models.CommitPutResult
	err := s.writeTx(ctx, func(tx *sql.Tx) error {
		var objectID int64
		var latest int64
		err := tx.QueryRowContext(ctx,
			`SELECT id, latest_version FROM objects WHERE bucket_id = ? AND object_key = ?`,
			in.BucketID, in.ObjectKey).Scan(&objectID, &latest)
		switch {
		case err == sql.ErrNoRows:
			now := in.Now
			if now == 0 {
				now = time.Now().Unix()
			}
			res, err := tx.ExecContext(ctx,
				`INSERT INTO objects (bucket_id, object_key, latest_version, created_at, updated_at)
				 VALUES (?, ?, 0, ?, ?)`, in.BucketID, in.ObjectKey, now, now)
			if err != nil {
				return err
			}
			if objectID, err = res.LastInsertId(); err != nil {
				return err
			}
			latest = 0
		case err != nil:
			return err
		}

		// 版本号严格递增且唯一：version = max(now, latest + 1)（design §18）。
		version := in.Version
		if version <= latest {
			version = latest + 1
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO object_versions
			 (object_id, version, data_file, data_offset, data_size, record_size,
			  filename, content_type, etag, checksum, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			objectID, version, in.DataFile, in.DataOffset, in.DataSize, in.RecordSize,
			nullableString(in.Filename), nullableString(in.ContentType),
			nullableString(in.ETag), nullableString(in.Checksum), in.Now,
		); err != nil {
			if isUniqueViolation(err) {
				return serr.NewError(serr.CodeVersionConflict)
			}
			return err
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE objects SET latest_version = ?, updated_at = ? WHERE id = ?`,
			version, in.Now, objectID); err != nil {
			return err
		}

		// 推进数据文件写游标（取最大值，天然幂等）。
		if _, err := tx.ExecContext(ctx,
			`UPDATE storage_files SET write_offset = MAX(write_offset, ?) WHERE filename = ?`,
			in.DataOffset+in.RecordSize, in.DataFile); err != nil {
			return err
		}

		result = models.CommitPutResult{ObjectID: objectID, Version: version}
		return nil
	})
	if err != nil {
		return models.CommitPutResult{}, s.finish(err)
	}
	return result, nil
}

// DeleteObject 删除对象及其全部版本元数据，不触碰 data.bin（design §41 / §42）。
func (s *Storage) DeleteObject(ctx context.Context, bucketID int64, objectKey string) (models.DeleteObjectResult, error) {
	var result models.DeleteObjectResult
	err := s.writeTx(ctx, func(tx *sql.Tx) error {
		var objectID int64
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM objects WHERE bucket_id = ? AND object_key = ?`,
			bucketID, objectKey).Scan(&objectID); err != nil {
			if err == sql.ErrNoRows {
				return serr.NewError(serr.CodeObjectNotFound)
			}
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM object_versions WHERE object_id = ?`, objectID)
		if err != nil {
			return err
		}
		deleted, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM objects WHERE id = ?`, objectID); err != nil {
			return err
		}
		result = models.DeleteObjectResult{ObjectID: objectID, DeletedVersions: deleted}
		return nil
	})
	if err != nil {
		return models.DeleteObjectResult{}, s.finish(err)
	}
	return result, nil
}

// MaxRecordEnd 返回 MAX(data_offset + record_size)，用于重建 write_cursor（design §11）。
func (s *Storage) MaxRecordEnd(ctx context.Context) (int64, error) {
	var maxEnd sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(data_offset + record_size) FROM object_versions`).Scan(&maxEnd)
	if err != nil {
		return 0, s.wrapErr(err)
	}
	if !maxEnd.Valid {
		return 0, nil
	}
	return maxEnd.Int64, nil
}

// SumRecordSize 返回全部有效版本 record_size 之和，用于 orphan 统计（design §44）。
func (s *Storage) SumRecordSize(ctx context.Context) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT SUM(record_size) FROM object_versions`).Scan(&total)
	if err != nil {
		return 0, s.wrapErr(err)
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Int64, nil
}

// CheckCorruption 检查是否存在指向数据文件之外的元数据记录（design §73）。
func (s *Storage) CheckCorruption(ctx context.Context, capacity int64) ([]models.ObjectVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, object_id, version, data_file, data_offset, data_size, record_size,
		 filename, content_type, etag, checksum, created_at
		 FROM object_versions WHERE data_offset + record_size > ? ORDER BY data_offset LIMIT 100`, capacity)
	if err != nil {
		return nil, s.wrapErr(err)
	}
	defer rows.Close()

	var out []models.ObjectVersion
	for rows.Next() {
		var (
			v                     models.ObjectVersion
			filename, contentType sql.NullString
			etag, checksum        sql.NullString
		)
		if err := rows.Scan(
			&v.ID, &v.ObjectID, &v.Version, &v.DataFile, &v.DataOffset, &v.DataSize, &v.RecordSize,
			&filename, &contentType, &etag, &checksum, &v.CreateTS,
		); err != nil {
			return nil, s.wrapErr(err)
		}
		v.Filename = filename.String
		v.ContentType = contentType.String
		v.ETag = etag.String
		v.Checksum = checksum.String
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, s.wrapErr(err)
	}
	return out, nil
}

// finish 统一收口：已经是 CustomError 的原样返回，其余包装为 METADATA_ERROR。
func (s *Storage) finish(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := serr.AsCustomError(err); ok {
		return err
	}
	return s.wrapErr(err)
}

// nullableString 把空字符串存为 NULL。
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
