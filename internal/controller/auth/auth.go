// Package auth 提供用户认证 HTTP 控制器。
// 职责仅限 HTTP 层：解析请求参数 → 调用 authsvc → 映射响应 DTO。
// 业务逻辑（bcrypt 校验、JWT 签发）已下沉至 internal/service/auth。
package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	v1 "SentinelOps/api/auth/v1"
	authsvc "SentinelOps/internal/service/auth"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// codeInvalidCredentials 是登录失败专用的 401 状态码。
var codeInvalidCredentials = gcode.New(http.StatusUnauthorized, "Invalid Credentials", nil)

type ControllerV1 struct{}

func NewV1() *ControllerV1 {
	return &ControllerV1{}
}

// Login 解析登录请求并委托 authsvc.Login 完成认证，返回 JWT Token 和用户基本信息。
func (c *ControllerV1) Login(ctx context.Context, req *v1.LoginReq) (*v1.LoginRes, error) {
	token, userID, role, username, err := authsvc.Login(ctx, req.Username, req.Password)
	if err != nil {
		return nil, mapAuthError(err)
	}
	return &v1.LoginRes{Token: token, UserID: userID, Role: role, Username: username}, nil
}

// Register 由管理员创建新用户（可指定角色），成功后直接签发 JWT Token。
func (c *ControllerV1) Register(ctx context.Context, req *v1.RegisterReq) (*v1.RegisterRes, error) {
	token, userID, role, username, err := authsvc.Register(ctx, req.Username, req.Password, req.Role)
	if err != nil {
		return nil, mapAuthError(err)
	}
	return &v1.RegisterRes{Token: token, UserID: userID, Role: role, Username: username}, nil
}

// ListUsers 返回用户管理列表；仅管理员可读，响应不含密码哈希。
func (c *ControllerV1) ListUsers(ctx context.Context, _ *v1.ListUsersReq) (*v1.ListUsersRes, error) {
	users, err := authsvc.ListUsers(ctx)
	if err != nil {
		return nil, mapAuthError(err)
	}
	items := make([]v1.UserItem, 0, len(users))
	for _, user := range users {
		items = append(items, v1.UserItem{
			ID:        user.ID,
			Username:  user.Username,
			Role:      user.Role,
			CreatedAt: user.CreatedAt.Format(time.RFC3339),
		})
	}
	return &v1.ListUsersRes{Items: items}, nil
}

// mapAuthError 把认证业务错误映射为真实 HTTP 状态，避免 200 + message 或
// 泄露 bcrypt/DAO 内部错误文本。
func mapAuthError(err error) error {
	switch {
	case errors.Is(err, authsvc.ErrInvalidCredentials):
		return gerror.NewCode(codeInvalidCredentials, authsvc.ErrInvalidCredentials.Error())
	case errors.Is(err, authsvc.ErrPasswordTooShort):
		return gerror.NewCode(gcode.CodeValidationFailed, authsvc.ErrPasswordTooShort.Error())
	case errors.Is(err, authsvc.ErrInvalidRole):
		return gerror.NewCode(gcode.CodeValidationFailed, authsvc.ErrInvalidRole.Error())
	default:
		return err
	}
}
