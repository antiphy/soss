// Package errors 提供统一的业务错误体系：业务码 + HTTP 状态映射 + 多语种文案。
package errors

import "strings"

// lang 当前默认语言，供 Error() 输出使用。
var lang = "en"

// SetLang 设置默认语言（"zh" / "en"），非线程安全，仅应在启动阶段调用。
func SetLang(l string) {
	switch strings.ToLower(l) {
	case "zh", "zh-cn", "zh_cn":
		lang = "zh"
	default:
		lang = "en"
	}
}

// CustomError 业务错误。业务层只应返回本类型，不要把驱动/系统原生错误直接冒泡。
type CustomError struct {
	Code Code
	Msg  string
}

// NewError 构造一个使用默认文案的错误。
func NewError(code Code) CustomError {
	return CustomError{Code: code, Msg: message(code)}
}

// NewErrorWithMsg 构造一个带动态上下文的错误。
func NewErrorWithMsg(code Code, msg string) CustomError {
	if msg == "" {
		msg = message(code)
	}
	return CustomError{Code: code, Msg: msg}
}

// Wrap 把任意错误包装成指定业务码，保留原始错误信息作为上下文。
func Wrap(code Code, err error) CustomError {
	if err == nil {
		return NewError(code)
	}
	return CustomError{Code: code, Msg: message(code) + ": " + err.Error()}
}

// Error 实现 error 接口。
func (e CustomError) Error() string {
	if e.Msg == "" {
		return CodeName(e.Code)
	}
	return CodeName(e.Code) + ": " + e.Msg
}

// Is 判定错误是否为指定业务码。
func (e CustomError) Is(target error) bool {
	if t, ok := target.(CustomError); ok {
		return t.Code == e.Code
	}
	return false
}

// IsErrorCode 判定任意 error 是否为指定业务码。
func IsErrorCode(err error, code Code) bool {
	if err == nil {
		return false
	}
	if ce, ok := err.(CustomError); ok {
		return ce.Code == code
	}
	// 兼容被 fmt.Errorf("%w") 包装的场景。
	if u, ok := err.(interface{ Unwrap() error }); ok {
		return IsErrorCode(u.Unwrap(), code)
	}
	return false
}

// AsCustomError 尝试把任意 error 转换为 CustomError。
func AsCustomError(err error) (CustomError, bool) {
	for err != nil {
		if ce, ok := err.(CustomError); ok {
			return ce, true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return CustomError{}, false
}

// CodeName 返回错误码的字符串标识，用于 API 错误体。
func CodeName(code Code) string {
	if m, ok := codeRegistry[code]; ok {
		return m.name
	}
	return NameInternalError
}

// HTTPStatus 返回错误码对应的 HTTP 状态码。
func HTTPStatus(code Code) int {
	if m, ok := codeRegistry[code]; ok {
		return m.http
	}
	return 500
}

// Message 返回错误码在当前语言下的默认文案。
func Message(code Code) string {
	return message(code)
}

func message(code Code) string {
	m, ok := codeRegistry[code]
	if !ok {
		if lang == "zh" {
			return MsgInternalErrorZH
		}
		return MsgInternalErrorEN
	}
	if lang == "zh" {
		return m.zh
	}
	return m.en
}
