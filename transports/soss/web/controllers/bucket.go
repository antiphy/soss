package controllers

import (
	"github.com/labstack/echo/v4"

	"github.com/favos-me/soss/logic"
	"github.com/favos-me/soss/models"
)

// MetricsGET 返回运行态指标（design §87）。
func MetricsGET(c echo.Context) error {
	return respondOK(c, models.Response{Code: 0, Data: logic.Logic.Stats()})
}
