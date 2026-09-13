// Package auth 提供用户认证 HTTP 控制器。
// 职责仅限 HTTP 层：解析请求参数 → 调用 authsvc → 映射响应 DTO。
// 业务逻辑（bcrypt 校验、JWT 签发）已下沉至 internal/service/auth。
package auth

import (
	"context"
	"errors"
	"net/http"

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

// Register 注册新用户，成功后直接签发 JWT Token。
func (c *ControllerV1) Register(ctx context.Context, req *v1.RegisterReq) (*v1.RegisterRes, error) {
	token, userID, role, username, err := authsvc.Register(ctx, req.Username, req.Password)
	if err != nil {
		return nil, mapAuthError(err)
	}
	return &v1.RegisterRes{Token: token, UserID: userID, Role: role, Username: username}, nil
}

// mapAuthError 把认证业务错误映射为真实 HTTP 状态，避免 200 + message 或
// 泄露 bcrypt/DAO 内部错误文本。
func mapAuthError(err error) error {
	switch {
	case errors.Is(err, authsvc.ErrInvalidCredentials):
		return gerror.NewCode(codeInvalidCredentials, authsvc.ErrInvalidCredentials.Error())
	case errors.Is(err, authsvc.ErrPasswordTooShort):
		return gerror.NewCode(gcode.CodeValidationFailed, authsvc.ErrPasswordTooShort.Error())
	default:
		return err
	}
}
