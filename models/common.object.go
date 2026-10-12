package models

// Response 统一成功响应外壳。
type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg,omitempty"`
	Data any    `json:"data,omitempty"`
}

// ErrorResponse 统一错误响应体（设计文档 §69）。
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Paging 分页结构。
type Paging struct {
	Page  int   `json:"page"`
	Size  int   `json:"size"`
	Total int64 `json:"total"`
}
