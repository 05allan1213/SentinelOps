package middleware

import (
	"errors"
	"net/http"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"
	"gorm.io/gorm"
)

// Response 统一 JSON 响应结构。
type Response struct {
	Message string      `json:"message" dc:"消息提示"`
	Data    interface{} `json:"data"    dc:"执行结果"`
}

// ResponseMiddleware 将 handler 返回值统一包装为 JSON 响应。
// SSE 流式响应（Content-Type: text/event-stream）已由 handler 直接写入，跳过包装。
// 文件下载响应（Content-Disposition: attachment）已由 handler 直接写入，跳过包装。
func ResponseMiddleware(r *ghttp.Request) {
	r.Middleware.Next()

	// 前置中间件（限流、鉴权）或 handler 已经写入响应体时，不能再追加统一信封，
	// 否则会输出两段拼接的非法 JSON（例如 429 的 code 报文 + OK 信封）。
	if r.Response.BufferLength() > 0 || r.Response.BytesWritten() > 0 {
		return
	}

	if r.Response.Header().Get("Content-Type") == "text/event-stream" {
		return
	}

	// 跳过文件下载响应
	if r.Response.Header().Get("Content-Disposition") != "" {
		return
	}

	var (
		msg string
		res = r.GetHandlerResponse()
		err = r.GetError()
	)
	if err != nil {
		msg = err.Error()
		// 已分类的错误使用真实 HTTP 状态；未分类业务错误保持 GoFrame
		// 200 + message 的既有兼容契约。
		if r.Response.Status == 0 || r.Response.Status == http.StatusOK {
			if status := classifiedErrorHTTPStatus(err); status != 0 {
				r.Response.WriteHeader(status)
			}
		}
	} else {
		msg = "OK"
	}
	r.Response.WriteJson(Response{
		Message: msg,
		Data:    res,
	})
}

// classifiedErrorHTTPStatus 把已经带上语义的 handler 错误映射为 HTTP 状态。
// 返回 0 表示该错误没有可判定的分类，调用方保持原有 200 响应。
func classifiedErrorHTTPStatus(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound
	}
	switch gerror.Code(err) {
	case gcode.CodeNotFound:
		return http.StatusNotFound
	case gcode.CodeValidationFailed:
		return http.StatusBadRequest
	case gcode.CodeNotAuthorized:
		return http.StatusForbidden
	case gcode.CodeNotSupported:
		return http.StatusNotImplemented
	case gcode.CodeOperationFailed, gcode.CodeInternalError:
		return http.StatusInternalServerError
	default:
		return 0
	}
}
