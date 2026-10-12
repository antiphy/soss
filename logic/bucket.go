package logic

import (
	"context"

	"go.uber.org/zap"

	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/consts/types"
	"github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/models"
	"github.com/favos-me/soss/utils"
)

// CreateBucket 创建存储桶（design §28）。
func (m *Manager) CreateBucket(ctx context.Context, req *models.BucketCreateReq) (*models.Bucket, error) {
	if req == nil {
		return nil, errors.NewError(errors.CodeInvalidArgument)
	}
	if err := utils.ValidateBucketName(req.Name); err != nil {
		return nil, errors.NewErrorWithMsg(errors.CodeInvalidArgument, err.Error())
	}
	class := req.Class
	if class == 0 {
		class = types.BucketClassNormal
	}
	if !class.Valid() {
		return nil, errors.NewErrorWithMsg(errors.CodeInvalidArgument, "bucket class must be 1(HOT) or 2(NORMAL)")
	}
	// HOT 必须受内存容量约束（design §21 / 原则八）。
	if class == types.BucketClassHot {
		stats := m.hot.Stats()
		if stats.MaxBytes > 0 && stats.UsedBytes > stats.MaxBytes {
			m.alarms.CacheCapacityExceeded("hot", stats.UsedBytes, stats.MaxBytes)
			return nil, errors.NewError(errors.CodeCacheCapacityExceeded)
		}
	}

	bucket := &models.Bucket{Name: req.Name, Class: class}
	if err := m.ds.CreateBucket(ctx, bucket); err != nil {
		return nil, err
	}
	m.invalidateBucket(req.Name)
	m.logger.Info("bucket created",
		zap.String(consts.LogFieldNameOperation, consts.OperationPutBucket),
		zap.String(consts.LogFieldNameBucket, bucket.Name),
		zap.String("class", class.String()),
		zap.Int64("id", bucket.ID),
	)
	return bucket, nil
}

// ListBuckets 列出全部存储桶（design §29）。
func (m *Manager) ListBuckets(ctx context.Context) ([]models.BucketView, error) {
	buckets, err := m.ds.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]models.BucketView, 0, len(buckets))
	for _, b := range buckets {
		view := models.BucketView{Bucket: b}
		if count, err := m.ds.CountObjects(ctx, b.ID); err == nil {
			view.ObjectCount = count
		}
		if total, err := m.ds.SumObjectPayloadSize(ctx, b.ID); err == nil {
			view.TotalBytes = total
		}
		views = append(views, view)
	}
	return views, nil
}

// GetBucket 查询单个存储桶（design §30）。
func (m *Manager) GetBucket(ctx context.Context, name string) (*models.BucketView, error) {
	b, err := m.getBucket(ctx, name)
	if err != nil {
		return nil, err
	}
	view := &models.BucketView{Bucket: b}
	if count, err := m.ds.CountObjects(ctx, b.ID); err == nil {
		view.ObjectCount = count
	}
	if total, err := m.ds.SumObjectPayloadSize(ctx, b.ID); err == nil {
		view.TotalBytes = total
	}
	return view, nil
}

// UpdateBucket 修改存储桶类型（design §31 / §32）。
func (m *Manager) UpdateBucket(ctx context.Context, name string, req *models.BucketUpdateReq) (*models.Bucket, error) {
	if req == nil || req.Class == nil {
		return nil, errors.NewErrorWithMsg(errors.CodeInvalidArgument, "class is required")
	}
	newClass := *req.Class
	if !newClass.Valid() {
		return nil, errors.NewErrorWithMsg(errors.CodeInvalidArgument, "bucket class must be 1(HOT) or 2(NORMAL)")
	}
	b, err := m.getBucket(ctx, name)
	if err != nil {
		return nil, err
	}
	if b.Class == newClass {
		return &b, nil
	}

	switch newClass {
	case types.BucketClassHot:
		// NORMAL -> HOT：先评估容量，再改 class，最后后台 warm-up。
		total, err := m.ds.SumObjectPayloadSize(ctx, b.ID)
		if err != nil {
			return nil, err
		}
		stats := m.hot.Stats()
		if stats.MaxBytes > 0 && stats.UsedBytes+total > stats.MaxBytes {
			m.alarms.CacheCapacityExceeded("hot", stats.UsedBytes, stats.MaxBytes)
			return nil, errors.NewErrorWithMsg(errors.CodeCacheCapacityExceeded,
				"hot cache cannot hold "+utils.FormatSize(total)+" of bucket data")
		}
	case types.BucketClassNormal:
		// HOT -> NORMAL：直接清理该桶的 HOT 缓存（design §32）。
		m.hot.DeleteBucket(b.ID)
	}

	if err := m.ds.UpdateBucketClass(ctx, b.ID, newClass); err != nil {
		return nil, err
	}
	b.Class = newClass
	m.invalidateBucket(name)

	if newClass == types.BucketClassHot {
		bucket := b
		m.goAsync(func() { m.warmUpBucket(bucket) })
	}

	m.logger.Info("bucket updated",
		zap.String(consts.LogFieldNameOperation, consts.OperationPatchBucket),
		zap.String(consts.LogFieldNameBucket, b.Name),
		zap.String("class", newClass.String()),
	)
	return &b, nil
}

// DeleteBucket 删除空桶；非空桶返回 409（design §33）。
func (m *Manager) DeleteBucket(ctx context.Context, name string) error {
	b, err := m.getBucket(ctx, name)
	if err != nil {
		return err
	}
	if err := m.ds.DeleteBucket(ctx, b.ID); err != nil {
		return err
	}
	m.invalidateBucket(name)
	m.hot.DeleteBucket(b.ID)
	m.lru.DeleteBucket(b.ID)
	m.logger.Info("bucket deleted",
		zap.String(consts.LogFieldNameOperation, consts.OperationDeleteBucket),
		zap.String(consts.LogFieldNameBucket, name),
	)
	return nil
}
