package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// LoginReq 登录请求
type LoginReq struct {
	g.Meta   `path:"/auth/v1/login" method:"post" summary:"登录"`
	Username string `json:"username" v:"required"`
	Password string `json:"password" v:"required"`
}

// LoginRes 登录响应
type LoginRes struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	Username string `json:"username"`
}

// RegisterReq 注册请求
type RegisterReq struct {
	g.Meta   `path:"/auth/v1/register" method:"post" summary:"注册"`
	Username string `json:"username" v:"required|length:3,32"`
	Password string `json:"password" v:"required|length:6,64"`
}

// RegisterRes 注册响应
type RegisterRes struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	Username string `json:"username"`
}
