// Package controllers 是 HTTP 请求的薄壳：绑定参数 -> 调 logic -> 写响应。
//
// 铁律：控制器不直连 dal / storage / cache，一切业务走 logic（SKILL.md §3）。
package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/favos-me/soss/consts"
	"github.com/favos-me/soss/errors"
	"github.com/favos-me/soss/models"
)

// fallbackLogger 未经过 RequestID 中间件时使用的兜底 logger。
var fallbackLogger *zap.Logger = zap.NewNop()

// SetLogger 注入兜底 logger。
func SetLogger(l *zap.Logger) {
	if l != nil {
		fallbackLogger = l
	}
}

// Logger 取出带 tid 的 logger。
func Logger(c echo.Context) *zap.Logger {
	if c != nil {
		if l, ok := c.Get(consts.ContextKeyLogger).(*zap.Logger); ok && l != nil {
			return l
		}
	}
	return fallbackLogger
}

// respondOK 返回 200 + JSON 数据。
func respondOK(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, data)
}

// respondErr 把业务错误转换为统一错误响应（design §69）。
func respondErr(c echo.Context, err error) error {
	ce, ok := errors.AsCustomError(err)
	if !ok {
		ce = errors.Wrap(errors.CodeInternalError, err)
	}
	Logger(c).Error("request business error", zap.Error(err))
	return c.JSON(errors.HTTPStatus(ce.Code), models.ErrorResponse{
		Code:    errors.CodeName(ce.Code),
		Message: ce.Msg,
	})
}

// ErrorHandler 全局 HTTPErrorHandler，兜底未被控制器处理的错误。
func ErrorHandler(logger *zap.Logger) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		he, ok := err.(*echo.HTTPError)
		if ok {
			code := errors.CodeInvalidArgument
			switch he.Code {
			case http.StatusNotFound:
				code = errors.CodeObjectNotFound
			case http.StatusMethodNotAllowed:
				code = errors.CodeInvalidArgument
			case http.StatusRequestEntityTooLarge:
				code = errors.CodeObjectTooLarge
			default:
				code = errors.CodeInvalidArgument
			}
			_ = c.JSON(he.Code, models.ErrorResponse{
				Code:    errors.CodeName(code),
				Message: messageOf(he),
			})
			return
		}
		ce, isCustom := errors.AsCustomError(err)
		if !isCustom {
			ce = errors.Wrap(errors.CodeInternalError, err)
		}
		logger.Error("unhandled error", zap.Error(err))
		_ = c.JSON(errors.HTTPStatus(ce.Code), models.ErrorResponse{
			Code:    errors.CodeName(ce.Code),
			Message: ce.Msg,
		})
	}
}

func messageOf(he *echo.HTTPError) string {
	if m, ok := he.Message.(string); ok {
		return m
	}
	return http.StatusText(he.Code)
}
