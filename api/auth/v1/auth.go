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

// RegisterReq 注册请求；仅管理员可调用，可显式指定新用户角色。
type RegisterReq struct {
	g.Meta   `path:"/auth/v1/register" method:"post" summary:"注册"`
	Username string `json:"username" v:"required|length:3,32"`
	Password string `json:"password" v:"required|length:6,64"`
	Role     string `json:"role"` // 可选：viewer/operator/approver/admin，默认 viewer
}

// RegisterRes 注册响应
type RegisterRes struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	Username string `json:"username"`
}

// UserItem 是用户管理列表项，绝不包含密码字段。
type UserItem struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// ListUsersReq 查询用户列表（仅管理员）。
type ListUsersReq struct {
	g.Meta `path:"/auth/v1/users" method:"get" summary:"用户列表"`
}

// ListUsersRes 用户列表响应。
type ListUsersRes struct {
	Items []UserItem `json:"items"`
}
