package middleware

import "github.com/gogf/gf/v2/net/ghttp"

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
	} else {
		msg = "OK"
	}
	r.Response.WriteJson(Response{
		Message: msg,
		Data:    res,
	})
}
