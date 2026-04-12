package middleware

import (
	"errors"
	"net/http"

	"github.com/WormW/auto-rss/internal/api/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// APIError API 错误接口
type APIError interface {
	Error() string
	Code() response.ResponseCode
	HTTPStatus() int
}

// apiError 实现 APIError 接口
type apiError struct {
	message    string
	code       response.ResponseCode
	httpStatus int
}

func (e *apiError) Error() string {
	return e.message
}

func (e *apiError) Code() response.ResponseCode {
	return e.code
}

func (e *apiError) HTTPStatus() int {
	return e.httpStatus
}

// NewAPIError 创建新的 API 错误
func NewAPIError(message string, code response.ResponseCode, httpStatus int) APIError {
	return &apiError{
		message:    message,
		code:       code,
		httpStatus: httpStatus,
	}
}

// ErrorHandler 全局错误处理中间件
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// 检查是否有错误
		if len(c.Errors) == 0 {
			return
		}

		// 获取最后一个错误
		err := c.Errors.Last()

		// 处理不同类型的错误
		var apiErr APIError
		if errors.As(err.Err, &apiErr) {
			// 自定义 API 错误
			c.JSON(apiErr.HTTPStatus(), response.Response{
				Code:    apiErr.Code(),
				Message: apiErr.Error(),
			})
			return
		}

		// 处理 GORM 错误
		if errors.Is(err.Err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "资源不存在")
			return
		}

		if errors.Is(err.Err, gorm.ErrDuplicatedKey) {
			response.Error(c, response.CodeDuplicate, "数据已存在")
			return
		}

		// 处理常见 HTTP 错误
		switch err.Type {
		case gin.ErrorTypeBind:
			// 绑定错误（JSON 解析失败等）
			response.BadRequest(c, "请求参数格式错误: "+err.Err.Error())
		case gin.ErrorTypeRender:
			// 渲染错误
			response.InternalError(c, "响应渲染失败")
		default:
			// 其他错误
			if gin.Mode() == gin.ReleaseMode {
				response.InternalError(c, "服务器内部错误")
			} else {
				response.ErrorWithDetail(c, response.CodeInternalError, "服务器内部错误", err.Err.Error())
			}
		}
	}
}

// RecoveryWithResponse 带标准响应的恢复中间件
func RecoveryWithResponse() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				// 记录 panic 信息
				var errMsg string
				if err, ok := r.(error); ok {
					errMsg = err.Error()
				} else {
					errMsg = "unknown panic"
				}

				// 返回标准错误响应
				if gin.Mode() == gin.ReleaseMode {
					c.JSON(http.StatusInternalServerError, response.Response{
						Code:    response.CodeInternalError,
						Message: "服务器内部错误",
					})
				} else {
					c.JSON(http.StatusInternalServerError, response.Response{
						Code:    response.CodeInternalError,
						Message: "服务器内部错误: " + errMsg,
					})
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}
