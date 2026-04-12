// Package response 提供统一的 API 响应格式
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ResponseCode 响应码类型
type ResponseCode int

// 标准响应码定义
const (
	CodeSuccess ResponseCode = 0
	CodeError   ResponseCode = 1

	// 认证相关错误码 1000-1099
	CodeUnauthorized  ResponseCode = 1000 // 未授权
	CodeForbidden     ResponseCode = 1001 // 禁止访问
	CodeTokenExpired  ResponseCode = 1002 // Token 过期
	CodeInvalidToken  ResponseCode = 1003 // 无效 Token
	CodeInvalidCredentials ResponseCode = 1004 // 无效凭据

	// 请求相关错误码 2000-2099
	CodeBadRequest    ResponseCode = 2000 // 请求参数错误
	CodeValidationErr ResponseCode = 2001 // 数据验证错误
	CodeNotFound      ResponseCode = 2002 // 资源不存在
	CodeConflict      ResponseCode = 2003 // 资源冲突

	// 服务器错误码 3000-3099
	CodeInternalError ResponseCode = 3000 // 内部服务器错误
	CodeDBError       ResponseCode = 3001 // 数据库错误
	CodeExternalError ResponseCode = 3002 // 外部服务错误
	CodeTimeout       ResponseCode = 3003 // 请求超时

	// 业务逻辑错误码 4000-4099
	CodeDuplicate    ResponseCode = 4000 // 重复数据
	CodeLimitReached ResponseCode = 4001 // 达到限制
	CodeNotAllowed   ResponseCode = 4002 // 操作不允许
)

// Response 标准 API 响应结构
type Response struct {
	Code    ResponseCode `json:"code"`              // 业务状态码
	Message string       `json:"message"`           // 提示信息
	Data    any          `json:"data,omitempty"`    // 响应数据
}

// ErrorResponse 错误响应结构
type ErrorResponse struct {
	Code       ResponseCode `json:"code"`                 // 业务状态码
	Message    string       `json:"message"`              // 提示信息
	Error      string       `json:"error,omitempty"`      // 错误详情（调试用）
	RequestID  string       `json:"request_id,omitempty"` // 请求 ID，用于追踪
}

// Success 返回成功响应
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{
		Code:    CodeSuccess,
		Message: "Success",
		Data:    data,
	})
}

// SuccessWithMessage 返回带自定义消息的成功响应
func SuccessWithMessage(c *gin.Context, message string, data any) {
	c.JSON(http.StatusOK, Response{
		Code:    CodeSuccess,
		Message: message,
		Data:    data,
	})
}

// Error 返回错误响应
func Error(c *gin.Context, code ResponseCode, message string) {
	c.JSON(getHTTPStatus(code), Response{
		Code:    code,
		Message: message,
	})
}

// ErrorWithData 返回带数据的错误响应
func ErrorWithData(c *gin.Context, code ResponseCode, message string, data any) {
	c.JSON(getHTTPStatus(code), Response{
		Code:    code,
		Message: message,
		Data:    data,
	})
}

// ErrorWithDetail 返回带错误详情的响应（调试用）
func ErrorWithDetail(c *gin.Context, code ResponseCode, message, detail string) {
	resp := ErrorResponse{
		Code:    code,
		Message: message,
	}

	// 仅在非生产环境返回错误详情
	if gin.Mode() != gin.ReleaseMode {
		resp.Error = detail
	}

	c.JSON(getHTTPStatus(code), resp)
}

// PaginatedData 分页数据结构
type PaginatedData struct {
	List     any   `json:"list"`      // 数据列表
	Total    int64 `json:"total"`     // 总记录数
	Page     int   `json:"page"`      // 当前页码
	PageSize int   `json:"page_size"` // 每页大小
}

// Pagination 返回分页响应
func Pagination(c *gin.Context, list any, total int64, page, pageSize int) {
	Success(c, PaginatedData{
		List:     list,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// getHTTPStatus 根据业务码获取 HTTP 状态码
func getHTTPStatus(code ResponseCode) int {
	switch {
	case code == CodeSuccess:
		return http.StatusOK
	case code >= 1000 && code < 2000:
		return http.StatusUnauthorized
	case code >= 2000 && code < 3000:
		return http.StatusBadRequest
	case code >= 3000 && code < 4000:
		return http.StatusInternalServerError
	case code >= 4000 && code < 5000:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// 便捷方法：预定义的错误响应

// BadRequest 返回 400 错误
func BadRequest(c *gin.Context, message string) {
	Error(c, CodeBadRequest, message)
}

// NotFound 返回 404 错误
func NotFound(c *gin.Context, message string) {
	Error(c, CodeNotFound, message)
}

// InternalError 返回 500 错误
func InternalError(c *gin.Context, message string) {
	Error(c, CodeInternalError, message)
}

// Unauthorized 返回 401 错误
func Unauthorized(c *gin.Context, message string) {
	Error(c, CodeUnauthorized, message)
}

// Forbidden 返回 403 错误
func Forbidden(c *gin.Context, message string) {
	Error(c, CodeForbidden, message)
}
