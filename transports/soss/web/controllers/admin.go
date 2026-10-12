package controllers

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/logic"
	"github.com/favos-me/soss/models"
	sossmw "github.com/favos-me/soss/transports/soss/web/middleware"
)

// ---------------------------------------------------------------------------
// 页面：登录 / 登出 / 桶列表 / 桶详情
// ---------------------------------------------------------------------------

// AdminLoginGET 渲染登录页；已登录则直接进入后台。
func AdminLoginGET(c echo.Context) error {
	if sossmw.AdminUserFromRequest(c, logic.Logic.Cfg()) != "" {
		return c.Redirect(http.StatusFound, "/admin/buckets")
	}
	return c.Render(http.StatusOK, adminTemplate("login.html"), loginData(""))
}

// AdminLoginPOST 校验账号密码，成功后下发 JWT Cookie。
func AdminLoginPOST(c echo.Context) error {
	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")

	if !sossmw.VerifyAdmin(username, password) {
		Logger(c).Warn("admin login failed", zap.String("user", username))
		return c.Render(http.StatusUnauthorized, adminTemplate("login.html"), loginData("用户名或密码错误"))
	}
	if err := sossmw.IssueAdminSession(c, logic.Logic.Cfg(), username); err != nil {
		return respondErr(c, err)
	}
	Logger(c).Info("admin login succeeded", zap.String("user", username))
	return c.Redirect(http.StatusFound, "/admin/buckets")
}

// AdminLogoutGET 清除会话 Cookie 并回到登录页。
func AdminLogoutGET(c echo.Context) error {
	sossmw.RevokeAdminSession(c)
	return c.Redirect(http.StatusFound, consts.AdminLoginPath)
}

// AdminBucketsGET 管理后台首页：存储桶列表。
func AdminBucketsGET(c echo.Context) error {
	views, err := logic.Logic.ListBuckets(c.Request().Context())
	if err != nil {
		return respondErr(c, err)
	}
	return renderAdmin(c, "buckets.html", map[string]any{
		"title":   "存储桶 · SOSS 管理后台",
		"buckets": views,
	})
}

// AdminBucketGET 存储桶详情页：桶信息 + 对象列表。
func AdminBucketGET(c echo.Context) error {
	ctx := c.Request().Context()
	name := c.Param("bucket")
	view, err := logic.Logic.GetBucket(ctx, name)
	if err != nil {
		return respondErr(c, err)
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	objects, err := logic.Logic.ListObjects(ctx, name, c.QueryParam("prefix"), limit)
	if err != nil {
		return respondErr(c, err)
	}
	return renderAdmin(c, "bucket.html", map[string]any{
		"title":   name + " · SOSS 管理后台",
		"bucket":  view,
		"objects": objects,
	})
}

// ---------------------------------------------------------------------------
// JSON API：bucket 增删改查（已从 /v1 迁到 /admin）
// ---------------------------------------------------------------------------

// AdminBucketListGET 列出全部存储桶。
func AdminBucketListGET(c echo.Context) error {
	views, err := logic.Logic.ListBuckets(c.Request().Context())
	if err != nil {
		return respondErr(c, err)
	}
	items := make([]models.BucketView, 0, len(views))
	items = append(items, views...)
	return respondOK(c, map[string]any{"items": items, "total": len(items)})
}

// AdminBucketCreatePOST 创建存储桶。
func AdminBucketCreatePOST(c echo.Context) error {
	var req models.BucketCreateReq
	if err := c.Bind(&req); err != nil {
		return respondErr(c, errors.NewErrorWithMsg(errors.CodeInvalidArgument, err.Error()))
	}
	bucket, err := logic.Logic.CreateBucket(c.Request().Context(), &req)
	if err != nil {
		return respondErr(c, err)
	}
	return c.JSON(http.StatusCreated, bucket)
}

// AdminBucketDetailGET 查询单个存储桶。
func AdminBucketDetailGET(c echo.Context) error {
	view, err := logic.Logic.GetBucket(c.Request().Context(), c.Param("bucket"))
	if err != nil {
		return respondErr(c, err)
	}
	return respondOK(c, view)
}

// AdminBucketUpdatePATCH 修改存储桶类型。
func AdminBucketUpdatePATCH(c echo.Context) error {
	var req models.BucketUpdateReq
	if err := c.Bind(&req); err != nil {
		return respondErr(c, errors.NewErrorWithMsg(errors.CodeInvalidArgument, err.Error()))
	}
	bucket, err := logic.Logic.UpdateBucket(c.Request().Context(), c.Param("bucket"), &req)
	if err != nil {
		return respondErr(c, err)
	}
	return respondOK(c, bucket)
}

// AdminBucketDeleteDELETE 删除空桶。
func AdminBucketDeleteDELETE(c echo.Context) error {
	if err := logic.Logic.DeleteBucket(c.Request().Context(), c.Param("bucket")); err != nil {
		return respondErr(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// JSON API：object 增删改查
// ---------------------------------------------------------------------------

// AdminObjectListGET 列举桶内对象，支持 ?prefix= 前缀过滤与 ?limit= 条数上限。
func AdminObjectListGET(c echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	res, err := logic.Logic.ListObjects(c.Request().Context(), c.Param("bucket"), c.QueryParam("prefix"), limit)
	if err != nil {
		return respondErr(c, err)
	}
	return respondOK(c, res)
}

// AdminObjectMetaGET 查询对象元信息；带 ?versions=1 时返回该对象的版本列表。
func AdminObjectMetaGET(c echo.Context) error {
	ctx := c.Request().Context()
	bucket := c.Param("bucket")
	key := objectKeyFromParam(c)
	if isVersionsQuery(c) {
		res, err := logic.Logic.ListVersions(ctx, bucket, key)
		if err != nil {
			return respondErr(c, err)
		}
		return respondOK(c, res)
	}
	res, err := logic.Logic.HeadObject(ctx, bucket, key, 0)
	if err != nil {
		return respondErr(c, err)
	}
	return respondOK(c, map[string]any{
		"bucket":       res.Bucket,
		"object_key":   res.ObjectKey,
		"version":      res.Version,
		"size":         res.Size,
		"content_type": res.ContentType,
		"etag":         res.ETag,
		"filename":     res.Filename,
		"created_at":   res.CreatedAt,
	})
}

// AdminObjectUploadPOST 上传对象，支持 multipart 表单与裸 body 两种写法。
//
// multipart：字段 file（文件）+ 可选 key（缺省取 URL 中的 key）。
// 裸 body：请求体即对象内容，key 取 URL。
// 对同一 key 重复上传会生成新版本（version 递增），即"覆盖写"。
func AdminObjectUploadPOST(c echo.Context) error {
	ctx := c.Request().Context()
	bucket := c.Param("bucket")
	key := objectKeyFromParam(c)

	maxSize := logic.Logic.Cfg().Server.MaxObjectSize.Int64()
	var (
		body        []byte
		filename    string
		contentType = c.Request().Header.Get(echo.HeaderContentType)
	)

	if strings.HasPrefix(contentType, echo.MIMEMultipartForm) {
		form, err := c.MultipartForm()
		if err != nil {
			return respondErr(c, errors.NewErrorWithMsg(errors.CodeInvalidArgument, err.Error()))
		}
		if keys := form.Value["key"]; len(keys) > 0 && strings.TrimSpace(keys[0]) != "" {
			key = strings.TrimSpace(keys[0])
		}
		files := form.File["file"]
		if len(files) == 0 {
			return respondErr(c, errors.NewErrorWithMsg(errors.CodeInvalidArgument, `multipart field "file" is required`))
		}
		f, err := files[0].Open()
		if err != nil {
			return respondErr(c, errors.Wrap(errors.CodeStorageError, err))
		}
		defer f.Close()
		body, err = io.ReadAll(io.LimitReader(f, maxSize+1))
		if err != nil {
			return respondErr(c, errors.Wrap(errors.CodeStorageError, err))
		}
		filename = files[0].Filename
		contentType = files[0].Header.Get(echo.HeaderContentType)
	} else {
		raw, err := io.ReadAll(io.LimitReader(c.Request().Body, maxSize+1))
		if err != nil {
			return respondErr(c, errors.Wrap(errors.CodeStorageError, err))
		}
		body = raw
	}
	if key == "" {
		return respondErr(c, errors.NewErrorWithMsg(errors.CodeInvalidArgument, "object key is required"))
	}
	if int64(len(body)) > maxSize {
		return respondErr(c, errors.NewError(errors.CodeObjectTooLarge))
	}

	res, err := logic.Logic.PutObject(ctx, &models.ObjectPutReq{
		Bucket:      bucket,
		ObjectKey:   key,
		Filename:    filename,
		ContentType: contentType,
		Body:        body,
	})
	if err != nil {
		return respondErr(c, err)
	}
	c.Response().Header().Set(consts.HeaderObjectVersion, strconv.FormatInt(res.Version, 10))
	return respondOK(c, res)
}

// AdminObjectDeleteDELETE 删除对象及其全部版本。
func AdminObjectDeleteDELETE(c echo.Context) error {
	res, err := logic.Logic.DeleteObject(c.Request().Context(), c.Param("bucket"), objectKeyFromParam(c))
	if err != nil {
		return respondErr(c, err)
	}
	return respondOK(c, res)
}

// ---------- 辅助 ----------

// adminTemplate 返回管理后台模板名（SKILL.md §2：views/<lang>/admin/）。
func adminTemplate(name string) string {
	return consts.DefaultViewLang + "/admin/" + name
}

// renderAdmin 渲染管理后台页面，统一注入公共变量。
func renderAdmin(c echo.Context, name string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	data["user"] = sossmw.AdminUser(c)
	data["version"] = consts.AppVersion
	if logic.Logic != nil {
		data["read_only"] = logic.Logic.ReadOnly()
	}
	return c.Render(http.StatusOK, adminTemplate(name), data)
}

// loginData 登录页渲染数据。
func loginData(errMsg string) map[string]any {
	return map[string]any{
		"title":   "登录 · SOSS 管理后台",
		"version": consts.AppVersion,
		"error":   errMsg,
	}
}

// isVersionsQuery 判断是否请求版本列表。
func isVersionsQuery(c echo.Context) bool {
	v := c.QueryParam("versions")
	return v == "1" || strings.EqualFold(v, "true")
}
