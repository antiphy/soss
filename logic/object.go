package logic

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/consts/types"
	"github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/models"
	"github.com/favos-me/soss/services/cache"
	"github.com/favos-me/soss/services/storage"
	"github.com/favos-me/soss/utils"
)

// PutObject 上传对象（design §35 / §59）。
//
// 严格顺序：分配 offset -> 写 data.bin -> fsync -> SQLite 提交 -> 更新缓存。
// 绝不允许先提交元数据再写数据，否则会出现"元数据存在但数据不存在"的反向不一致。
func (m *Manager) PutObject(ctx context.Context, req *models.ObjectPutReq) (*models.ObjectPutRes, error) {
	if m.ReadOnly() {
		return nil, errors.NewError(errors.CodeServiceReadOnly)
	}
	if req == nil {
		return nil, errors.NewError(errors.CodeInvalidArgument)
	}
	if err := utils.ValidateObjectKey(req.ObjectKey, consts.MaxObjectKeyBytes); err != nil {
		return nil, errors.NewErrorWithMsg(errors.CodeInvalidArgument, err.Error())
	}
	size := int64(len(req.Body))
	if size > m.cfg.Server.MaxObjectSize.Int64() {
		return nil, errors.NewErrorWithMsg(errors.CodeObjectTooLarge,
			fmt.Sprintf("object size %s exceeds limit %s",
				utils.FormatSize(size), m.cfg.Server.MaxObjectSize.String()))
	}

	bucket, err := m.getBucket(ctx, req.Bucket)
	if err != nil {
		return nil, err
	}
	// HOT 桶必须先确认内存容量（design §21）。
	if bucket.Class == types.BucketClassHot && !m.hot.CanAdmit(size) {
		stats := m.hot.Stats()
		m.alarms.CacheCapacityExceeded("hot", stats.UsedBytes, stats.MaxBytes)
		return nil, errors.NewError(errors.CodeCacheCapacityExceeded)
	}

	// 同一个 object key 的写入串行化，保证版本号严格递增（design §58）。
	lockKey := cache.ObjectKey(bucket.ID, req.ObjectKey)
	m.keyMu.Lock(lockKey)
	defer m.keyMu.Unlock(lockKey)

	now := time.Now().Unix()
	latest := int64(0)
	if view, err := m.ds.LookupVersion(ctx, bucket.ID, bucket.Name, req.ObjectKey, 0); err == nil {
		latest = view.LatestVersion
	} else if !isNotFound(err) {
		return nil, err
	}
	// version = max(now, latest + 1)（design §18）。
	candidate := now
	if candidate <= latest {
		candidate = latest + 1
	}

	recordSize := int64(storage.HeaderSize) + size
	offset, err := m.store.Allocate(recordSize)
	if err != nil {
		return nil, err
	}

	// 1) 写 data.bin（append-only，永不原地修改）。
	if err := m.store.WriteRecord(offset, candidate, req.Body); err != nil {
		return nil, err
	}
	// 2) fsync：数据必须先于元数据持久化。
	if err := m.store.Sync(); err != nil {
		return nil, err
	}

	etag := utils.ETag(req.Body)
	checksum := utils.SHA256Hex(req.Body)
	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 3) SQLite 提交元数据（事务内再次校正版本号）。
	result, err := m.ds.CommitPut(ctx, models.CommitPutInput{
		BucketID:    bucket.ID,
		ObjectKey:   req.ObjectKey,
		Version:     candidate,
		DataFile:    m.store.DataFileName(),
		DataOffset:  offset,
		DataSize:    size,
		RecordSize:  recordSize,
		Filename:    req.Filename,
		ContentType: contentType,
		ETag:        etag,
		Checksum:    checksum,
		Now:         now,
	})
	if err != nil {
		// Case B：data.bin 写成功但元数据提交失败 -> 允许，成为孤儿数据（design §13）。
		m.logger.Error("commit metadata failed, orphan data left in data.bin",
			zap.String(consts.LogFieldNameOperation, consts.OperationPutObject),
			zap.String(consts.LogFieldNameBucket, req.Bucket),
			zap.String(consts.LogFieldNameObjectKey, req.ObjectKey),
			zap.Int64(consts.LogFieldNameOffset, offset),
			zap.Error(err),
		)
		return nil, err
	}
	if result.Version != candidate {
		// 版本号被事务校正，回填 header 中的版本字段（SQLite 始终为权威）。
		if err := m.store.PatchRecordVersion(offset, result.Version); err != nil {
			m.logger.Warn("patch record version failed", zap.Error(err))
		} else if err := m.store.Sync(); err != nil {
			m.logger.Warn("sync after patch version failed", zap.Error(err))
		}
	}

	// 4) 提交成功后才允许更新缓存（design §27 / 原则四）。
	entry := &cache.CacheEntry{
		BucketID:    bucket.ID,
		ObjectID:    result.ObjectID,
		ObjectKey:   req.ObjectKey,
		Version:     result.Version,
		Size:        size,
		Filename:    req.Filename,
		ContentType: contentType,
		ETag:        etag,
		Checksum:    checksum,
		Data:        req.Body,
	}
	switch bucket.Class {
	case types.BucketClassHot:
		if err := m.hot.Put(entry); err != nil {
			stats := m.hot.Stats()
			m.alarms.CacheCapacityExceeded("hot", stats.UsedBytes, stats.MaxBytes)
			m.logger.Warn("put object into hot cache failed",
				zap.String(consts.LogFieldNameBucket, req.Bucket),
				zap.String(consts.LogFieldNameObjectKey, req.ObjectKey),
				zap.Error(err),
			)
		}
	case types.BucketClassNormal:
		// 覆盖写：删除旧版本缓存条目，下次 GET 自动重新加载。
		m.lru.InvalidateObject(bucket.ID, req.ObjectKey)
	}
	m.setLatestVersion(bucket.ID, req.ObjectKey, result.Version)

	m.liveBytes.Add(recordSize)
	m.store.SetLiveRecordBytes(m.liveBytes.Load())
	m.metrics.Incr(consts.MetricPutBytesTotal, size)
	m.triggerCapacityCheck()

	m.logger.Info("object put",
		zap.String(consts.LogFieldNameOperation, consts.OperationPutObject),
		zap.String(consts.LogFieldNameBucket, req.Bucket),
		zap.String(consts.LogFieldNameObjectKey, req.ObjectKey),
		zap.Int64(consts.LogFieldNameVersion, result.Version),
		zap.Int64(consts.LogFieldNameSize, size),
		zap.Int64(consts.LogFieldNameOffset, offset),
	)

	return &models.ObjectPutRes{
		Bucket:      req.Bucket,
		ObjectKey:   req.ObjectKey,
		Version:     result.Version,
		Size:        size,
		ETag:        etag,
		ContentType: contentType,
		Filename:    req.Filename,
		CreatedAt:   now,
	}, nil
}

// GetObject 读取对象；version <= 0 表示读取最新版本（design §37 / §38 / §81）。
func (m *Manager) GetObject(ctx context.Context, bucketName, objectKey string, version int64) (*models.ObjectGetRes, error) {
	if version < 0 {
		version = 0
	}
	bucket, err := m.getBucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	resolved := version
	if resolved <= 0 {
		if v, ok := m.latestVersion(bucket.ID, objectKey); ok {
			resolved = v
		}
	}
	cacheControl := m.cacheControlFor(bucket.Class)

	// 1) HOT 缓存：命中则完全不读 SQLite 与磁盘（design §20）。
	if bucket.Class == types.BucketClassHot {
		if entry, ok := m.hot.Get(bucket.ID, objectKey); ok && (resolved == 0 || entry.Version == resolved) {
			m.metrics.Incr(consts.MetricHotCacheHit, 1)
			cached := entry
			res := m.buildGetRes(bucketName, &cached, consts.CacheHotHit)
			res.CacheControl = cacheControl
			return res, nil
		}
		m.metrics.Incr(consts.MetricHotCacheMiss, 1)
	}

	// 2) LRU 缓存（design §25）。
	if bucket.Class == types.BucketClassNormal && resolved > 0 {
		if entry, ok := m.lru.Get(cache.CacheKey(bucket.ID, objectKey, resolved)); ok {
			m.metrics.Incr(consts.MetricLRUHit, 1)
			cached := entry
			res := m.buildGetRes(bucketName, &cached, consts.CacheLRUHit)
			res.CacheControl = cacheControl
			return res, nil
		}
		m.metrics.Incr(consts.MetricLRUMiss, 1)
	}

	// 3) 回源加载，singleflight 防止缓存击穿（design §26）。
	sfKey := fmt.Sprintf("%d/%s/%d", bucket.ID, objectKey, resolved)
	value, err, _ := m.sfGroup.Do(sfKey, func() (any, error) {
		return m.loadFromStorage(ctx, bucket.ID, bucket.Name, objectKey, resolved)
	})
	if err != nil {
		return nil, err
	}
	res := value.(*models.ObjectGetRes)

	// 4) 回填缓存。
	if resolved <= 0 || res.Version == resolved {
		m.setLatestVersion(bucket.ID, objectKey, res.Version)
	}
	switch bucket.Class {
	case types.BucketClassHot:
		// HOT 只缓存当前版本。
		if res.Version >= res.LatestVersion {
			entry := &cache.CacheEntry{
				BucketID:    bucket.ID,
				ObjectKey:   objectKey,
				Version:     res.Version,
				Size:        res.Size,
				Filename:    res.Filename,
				ContentType: res.ContentType,
				ETag:        res.ETag,
				Checksum:    res.Checksum,
				Data:        res.Data,
			}
			if err := m.hot.Put(entry); err != nil {
				m.logger.Warn("fill hot cache failed",
					zap.String(consts.LogFieldNameBucket, bucketName),
					zap.String(consts.LogFieldNameObjectKey, objectKey),
					zap.Error(err))
			}
		}
		res.CacheFlag = consts.CacheHotMiss
	case types.BucketClassNormal:
		entry := &cache.CacheEntry{
			BucketID:    bucket.ID,
			ObjectKey:   objectKey,
			Version:     res.Version,
			Size:        res.Size,
			Filename:    res.Filename,
			ContentType: res.ContentType,
			ETag:        res.ETag,
			Checksum:    res.Checksum,
			Data:        res.Data,
		}
		m.lru.Put(entry)
		res.CacheFlag = consts.CacheLRUMiss
	}
	res.CacheControl = cacheControl
	return res, nil
}

// HeadObject 只返回元数据，不读取对象内容（design §39）。
func (m *Manager) HeadObject(ctx context.Context, bucketName, objectKey string, version int64) (*models.ObjectGetRes, error) {
	if version < 0 {
		version = 0
	}
	bucket, err := m.getBucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	view, err := m.ds.LookupVersion(ctx, bucket.ID, bucket.Name, objectKey, version)
	if err != nil {
		return nil, err
	}
	m.setLatestVersion(bucket.ID, objectKey, view.LatestVersion)
	return &models.ObjectGetRes{
		Bucket:       bucketName,
		ObjectKey:    objectKey,
		Version:      view.Version,
		Size:         view.DataSize,
		ContentType:  view.ContentType,
		ETag:         view.ETag,
		Filename:     view.Filename,
		Checksum:     view.Checksum,
		CreatedAt:    view.CreateTS,
		CacheFlag:    "META",
		CacheControl: m.cacheControlFor(bucket.Class),
	}, nil
}

// cacheControlFor 按 bucket class 返回响应缓存策略（design §56）。
func (m *Manager) cacheControlFor(class types.BucketClass) string {
	if class == types.BucketClassHot {
		return m.cfg.Server.CacheControlHot
	}
	return m.cfg.Server.CacheControlNormal
}

// DeleteObject 删除对象元数据与缓存，不删除 data.bin 内容（design §41 / §42）。
func (m *Manager) DeleteObject(ctx context.Context, bucketName, objectKey string) (*models.ObjectDeleteRes, error) {
	if m.ReadOnly() {
		return nil, errors.NewError(errors.CodeServiceReadOnly)
	}
	bucket, err := m.getBucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	result, err := m.ds.DeleteObject(ctx, bucket.ID, objectKey)
	if err != nil {
		return nil, err
	}
	m.hot.Delete(bucket.ID, objectKey)
	m.lru.InvalidateObject(bucket.ID, objectKey)
	m.deleteLatestVersion(bucket.ID, objectKey)
	m.refreshLiveBytes(ctx)

	m.logger.Info("object deleted",
		zap.String(consts.LogFieldNameOperation, consts.OperationDeleteObject),
		zap.String(consts.LogFieldNameBucket, bucketName),
		zap.String(consts.LogFieldNameObjectKey, objectKey),
		zap.Int64("deleted_versions", result.DeletedVersions),
	)
	return &models.ObjectDeleteRes{
		Bucket:     bucketName,
		ObjectKey:  objectKey,
		DeletedVer: result.DeletedVersions,
	}, nil
}

// ListObjects 列举桶内对象的当前版本（管理后台用）。
//
// prefix 为空表示不过滤；limit <= 0 时取默认上限 consts.MaxAdminListObjects。
// 结果按 object key 升序，超过 limit 的部分被截断并通过 Truncated 标记。
func (m *Manager) ListObjects(ctx context.Context, bucketName, prefix string, limit int) (*models.ObjectListRes, error) {
	if limit <= 0 {
		limit = consts.MaxAdminListObjects
	}
	bucket, err := m.getBucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	views, err := m.ds.ListLatestVersions(ctx, bucket.ID, bucket.Name)
	if err != nil {
		return nil, err
	}
	items := make([]models.ObjectItem, 0, len(views))
	for _, v := range views {
		if prefix != "" && !strings.HasPrefix(v.ObjectKey, prefix) {
			continue
		}
		items = append(items, models.ObjectItem{
			ObjectKey:   v.ObjectKey,
			Version:     v.Version,
			Size:        v.DataSize,
			ContentType: v.ContentType,
			Filename:    v.Filename,
			ETag:        v.ETag,
			CreatedAt:   v.CreateTS,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ObjectKey < items[j].ObjectKey })
	truncated := false
	if len(items) > limit {
		items = items[:limit]
		truncated = true
	}
	return &models.ObjectListRes{
		Bucket:    bucket.Name,
		Prefix:    prefix,
		Items:     items,
		Total:     len(items),
		Truncated: truncated,
	}, nil
}

// ListVersions 列出对象的全部版本（design §40）。
func (m *Manager) ListVersions(ctx context.Context, bucketName, objectKey string) (*models.VersionListRes, error) {
	bucket, err := m.getBucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	versions, err := m.ds.ListVersions(ctx, bucket.ID, objectKey)
	if err != nil {
		return nil, err
	}
	return &models.VersionListRes{
		Bucket:   bucketName,
		Object:   objectKey,
		Versions: versions,
		Total:    len(versions),
	}, nil
}

// ---------- 内部辅助 ----------

// loadFromStorage 从 SQLite + data.bin 加载对象内容（缓存未命中路径）。
func (m *Manager) loadFromStorage(ctx context.Context, bucketID int64, bucketName, objectKey string, version int64) (*models.ObjectGetRes, error) {
	view, err := m.ds.LookupVersion(ctx, bucketID, bucketName, objectKey, version)
	if err != nil {
		return nil, err
	}
	data, err := m.store.ReadRecord(view.DataOffset, view.RecordSize, m.cfg.Storage.VerifyOnRead)
	if err != nil {
		// 对象存在但存储层异常：必须返回 500，而不是 404（design §85）。
		m.metrics.Incr(consts.MetricStorageReadError, 1)
		m.alarms.StorageReadError(err, view.DataOffset, view.RecordSize)
		m.logger.Error("read object data failed",
			zap.String(consts.LogFieldNameOperation, consts.OperationGetObject),
			zap.String(consts.LogFieldNameBucket, bucketName),
			zap.String(consts.LogFieldNameObjectKey, objectKey),
			zap.Int64(consts.LogFieldNameOffset, view.DataOffset),
			zap.Error(err),
		)
		return nil, err
	}
	return &models.ObjectGetRes{
		Bucket:        bucketName,
		ObjectKey:     objectKey,
		Version:       view.Version,
		LatestVersion: view.LatestVersion,
		Size:          view.DataSize,
		Data:          data,
		ContentType:   view.ContentType,
		ETag:          view.ETag,
		Filename:      view.Filename,
		Checksum:      view.Checksum,
		CreatedAt:     view.CreateTS,
		CacheFlag:     consts.CacheDiskRead,
	}, nil
}

func (m *Manager) buildGetRes(bucketName string, entry *cache.CacheEntry, flag string) *models.ObjectGetRes {
	return &models.ObjectGetRes{
		Bucket:      bucketName,
		ObjectKey:   entry.ObjectKey,
		Version:     entry.Version,
		Size:        entry.Size,
		Data:        entry.Data,
		ContentType: entry.ContentType,
		ETag:        entry.ETag,
		Filename:    entry.Filename,
		Checksum:    entry.Checksum,
		CacheFlag:   flag,
	}
}

func (m *Manager) refreshLiveBytes(ctx context.Context) {
	live, err := m.ds.SumRecordSize(ctx)
	if err != nil {
		m.logger.Warn("refresh live record bytes failed", zap.Error(err))
		return
	}
	m.liveBytes.Store(live)
	m.store.SetLiveRecordBytes(live)
}

func (m *Manager) latestVersion(bucketID int64, objectKey string) (int64, bool) {
	if v, ok := m.latestVer.Load(cache.ObjectKey(bucketID, objectKey)); ok {
		return v.(int64), true
	}
	return 0, false
}

func (m *Manager) setLatestVersion(bucketID int64, objectKey string, version int64) {
	m.latestVer.Store(cache.ObjectKey(bucketID, objectKey), version)
}

func (m *Manager) deleteLatestVersion(bucketID int64, objectKey string) {
	m.latestVer.Delete(cache.ObjectKey(bucketID, objectKey))
}

func isNotFound(err error) bool {
	return errors.IsErrorCode(err, errors.CodeObjectNotFound) ||
		errors.IsErrorCode(err, errors.CodeVersionNotFound)
}
