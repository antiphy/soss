package controllers

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/logic"
	"github.com/favos-me/soss/models"
)

// versionsSuffix 版本列表路由后缀（design §40）。
//
// 注意：v1 采用 "对象 key 以 /versions 结尾则视为版本列表" 的约定，
// 因此名为 "xxx/versions" 的对象会被该路由遮蔽，后续版本可改为查询参数形式。
const versionsSuffix = "/versions"

// ObjectPutPUT 上传对象（design §34）。
func ObjectPutPUT(c echo.Context) error {
	ctx := c.Request().Context()

	bucketName := c.Param("bucket")
	key := objectKeyFromParam(c)
	if key == "" {
		return respondErr(c, errors.NewErrorWithMsg(errors.CodeInvalidArgument, "object key is required"))
	}

	maxSize := logic.Logic.Cfg().Server.MaxObjectSize.Int64()
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxSize+1))
	if err != nil {
		return respondErr(c, errors.Wrap(errors.CodeStorageError, err))
	}
	if int64(len(body)) > maxSize {
		return respondErr(c, errors.NewError(errors.CodeObjectTooLarge))
	}

	contentType := c.Request().Header.Get(echo.HeaderContentType)
	req := &models.ObjectPutReq{
		Bucket:      bucketName,
		ObjectKey:   key,
		Filename:    c.Request().Header.Get(consts.HeaderFilename),
		ContentType: contentType,
		Body:        body,
	}
	res, err := logic.Logic.PutObject(ctx, req)
	if err != nil {
		return respondErr(c, err)
	}
	c.Response().Header().Set(consts.HeaderObjectVersion, strconv.FormatInt(res.Version, 10))
	return respondOK(c, res)
}

// ObjectGetGET 下载对象，支持 ?version= 指定版本（design §37 / §38）。
func ObjectGetGET(c echo.Context) error {
	ctx := c.Request().Context()

	bucketName := c.Param("bucket")
	key := objectKeyFromParam(c)
	if strings.HasSuffix(key, versionsSuffix) {
		return objectVersions(c, bucketName, strings.TrimSuffix(key, versionsSuffix))
	}
	version, err := parseVersion(c.QueryParam("version"))
	if err != nil {
		return respondErr(c, err)
	}
	res, err := logic.Logic.GetObject(ctx, bucketName, key, version)
	if err != nil {
		return respondErr(c, err)
	}
	writeObjectHeaders(c, res)

	// If-None-Match -> 304，显著降低静态资源重复传输（design §55）。
	if match := trimETag(c.Request().Header.Get(consts.HeaderIfNoneMatch)); match != "" && match == res.ETag {
		return c.NoContent(http.StatusNotModified)
	}
	return c.Blob(http.StatusOK, defaultContentType(res.ContentType), res.Data)
}

// ObjectHeadHEAD 只返回对象元数据（design §39）。
func ObjectHeadHEAD(c echo.Context) error {
	ctx := c.Request().Context()

	version, err := parseVersion(c.QueryParam("version"))
	if err != nil {
		return respondErr(c, err)
	}
	res, err := logic.Logic.HeadObject(ctx, c.Param("bucket"), objectKeyFromParam(c), version)
	if err != nil {
		return respondErr(c, err)
	}
	writeObjectHeaders(c, res)
	c.Response().Header().Set(echo.HeaderContentLength, strconv.FormatInt(res.Size, 10))
	if match := trimETag(c.Request().Header.Get(consts.HeaderIfNoneMatch)); match != "" && match == res.ETag {
		return c.NoContent(http.StatusNotModified)
	}
	return c.NoContent(http.StatusOK)
}

// ObjectDeleteDELETE 删除对象（design §41）。
func ObjectDeleteDELETE(c echo.Context) error {
	ctx := c.Request().Context()

	res, err := logic.Logic.DeleteObject(ctx, c.Param("bucket"), objectKeyFromParam(c))
	if err != nil {
		return respondErr(c, err)
	}
	return respondOK(c, res)
}

func objectVersions(c echo.Context, bucketName, key string) error {
	ctx := c.Request().Context()

	res, err := logic.Logic.ListVersions(ctx, bucketName, key)
	if err != nil {
		return respondErr(c, err)
	}
	return respondOK(c, res)
}

// ---------- 辅助 ----------

// objectKeyFromParam 从 Echo 通配路由中还原 object key。
//
// Echo 已按 RawPath/Path 规则把通配段解码好，这里不能再 PathUnescape 一次：
// 多解一层会把 key 里字面的 %2F 当成斜杠，写路径会静默改名、读路径直接 404。
func objectKeyFromParam(c echo.Context) string {
	key := c.Param("*")
	key = strings.TrimPrefix(key, "/")
	key = strings.TrimSuffix(key, "/")
	return key
}

func parseVersion(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, errors.NewErrorWithMsg(errors.CodeInvalidArgument, "version must be a positive integer")
	}
	return v, nil
}

func writeObjectHeaders(c echo.Context, res *models.ObjectGetRes) {
	header := c.Response().Header()
	header.Set(echo.HeaderContentType, defaultContentType(res.ContentType))
	if res.ETag != "" {
		header.Set(consts.HeaderETag, `"`+res.ETag+`"`)
	}
	header.Set(consts.HeaderObjectVersion, strconv.FormatInt(res.Version, 10))
	if res.CacheControl != "" {
		header.Set(consts.HeaderCacheControl, res.CacheControl)
	}
	if res.CreatedAt > 0 {
		header.Set(consts.HeaderLastModified, time.Unix(res.CreatedAt, 0).UTC().Format(http.TimeFormat))
	}
	if res.Filename != "" {
		header.Set(consts.HeaderFilename, res.Filename)
		header.Set(consts.HeaderContentDisposition, mime.FormatMediaType("inline", map[string]string{"filename": res.Filename}))
	}
	if res.CacheFlag != "" {
		header.Set("X-Cache", res.CacheFlag)
	}
}

func defaultContentType(v string) string {
	if v == "" {
		return echo.MIMEOctetStream
	}
	return v
}

func trimETag(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "W/")
	return strings.Trim(v, `"`)
}
