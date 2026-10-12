package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/favos-me/soss/consts/types"
	serr "github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/models"
)

// CreateBucket 创建存储桶。
func (s *Storage) CreateBucket(ctx context.Context, b *models.Bucket) error {
	now := time.Now().Unix()
	err := s.writeTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO buckets (name, class, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			b.Name, int(b.Class), now, now)
		if err != nil {
			if isUniqueViolation(err) {
				return serr.NewError(serr.CodeBucketExists)
			}
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		b.ID = id
		b.CreateTS = now
		b.UpdateTS = now
		return nil
	})
	return s.finish(err)
}

// GetBucketByName 按名称查询存储桶。
func (s *Storage) GetBucketByName(ctx context.Context, name string) (models.Bucket, error) {
	return s.getBucket(ctx, `WHERE name = ?`, name)
}

// GetBucketByID 按 ID 查询存储桶。
func (s *Storage) GetBucketByID(ctx context.Context, id int64) (models.Bucket, error) {
	return s.getBucket(ctx, `WHERE id = ?`, id)
}

func (s *Storage) getBucket(ctx context.Context, where string, args ...any) (models.Bucket, error) {
	var b models.Bucket
	var class int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, class, created_at, updated_at FROM buckets `+where, args...,
	).Scan(&b.ID, &b.Name, &class, &b.CreateTS, &b.UpdateTS)
	if err != nil {
		return models.Bucket{}, s.notFound(err, serr.CodeBucketNotFound)
	}
	b.Class = types.BucketClass(class)
	return b, nil
}

// ListBuckets 返回全部存储桶。
func (s *Storage) ListBuckets(ctx context.Context) ([]models.Bucket, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, class, created_at, updated_at FROM buckets ORDER BY id`)
	if err != nil {
		return nil, s.wrapErr(err)
	}
	defer rows.Close()

	out := make([]models.Bucket, 0, 16)
	for rows.Next() {
		var b models.Bucket
		var class int
		if err := rows.Scan(&b.ID, &b.Name, &class, &b.CreateTS, &b.UpdateTS); err != nil {
			return nil, s.wrapErr(err)
		}
		b.Class = types.BucketClass(class)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, s.wrapErr(err)
	}
	return out, nil
}

// UpdateBucketClass 修改存储桶类型。
func (s *Storage) UpdateBucketClass(ctx context.Context, id int64, class types.BucketClass) error {
	err := s.writeTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE buckets SET class = ?, updated_at = ? WHERE id = ?`,
			int(class), time.Now().Unix(), id)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return serr.NewError(serr.CodeBucketNotFound)
		}
		return nil
	})
	return s.finish(err)
}

// DeleteBucket 删除空桶；非空桶返回 CodeBucketNotEmpty（design §33）。
func (s *Storage) DeleteBucket(ctx context.Context, id int64) error {
	err := s.writeTx(ctx, func(tx *sql.Tx) error {
		var count int64
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM objects WHERE bucket_id = ?`, id).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return serr.NewErrorWithMsg(serr.CodeBucketNotEmpty, "bucket contains objects")
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM buckets WHERE id = ?`, id)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return serr.NewError(serr.CodeBucketNotFound)
		}
		return nil
	})
	return s.finish(err)
}

// CountObjects 统计桶内对象数量。
func (s *Storage) CountObjects(ctx context.Context, bucketID int64) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM objects WHERE bucket_id = ?`, bucketID).Scan(&count)
	if err != nil {
		return 0, s.wrapErr(err)
	}
	return count, nil
}

// SumObjectPayloadSize 统计桶内当前版本对象的有效字节数（HOT 容量评估用）。
func (s *Storage) SumObjectPayloadSize(ctx context.Context, bucketID int64) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT SUM(v.data_size) FROM objects o
		 JOIN object_versions v ON v.object_id = o.id AND v.version = o.latest_version
		 WHERE o.bucket_id = ?`, bucketID).Scan(&total)
	if err != nil {
		return 0, s.wrapErr(err)
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Int64, nil
}
