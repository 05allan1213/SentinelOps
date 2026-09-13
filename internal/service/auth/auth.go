// Package authsvc 提供用户认证业务逻辑。
// 职责：校验密码 → 签发 JWT，不含 HTTP 层细节。
package authsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/utility/auth"

	"github.com/gogf/gf/v2/frame/g"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	// ErrInvalidCredentials 是登录失败唯一对外错误：用户不存在与密码错误必须
	// 返回同一消息，既防止账号枚举，也不把 bcrypt/DAO 内部细节暴露给客户端。
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	// ErrPasswordTooShort 由 HTTP 层映射为 400，而不是 200 + message。
	ErrPasswordTooShort = errors.New("密码长度不能少于 6 位")
	// ErrInvalidRole 表示管理员提交了不受支持的新用户角色。
	ErrInvalidRole = errors.New("用户角色无效")
)

// Login 校验用户名/密码，成功后签发 JWT Token。
// 密码错误与用户不存在使用相同错误消息，防止枚举攻击。
// 过期时长从配置 auth.jwt.expire_hours 读取，未配置时默认 24 小时。
func Login(ctx context.Context, username, password string) (token, userID, role, uname string, err error) {
	user, err := dao.FindUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", "", "", ErrInvalidCredentials
		}
		return "", "", "", "", err
	}
	if err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return "", "", "", "", ErrInvalidCredentials
	}
	token, userID, role, err = issueToken(ctx, user.ID, user.Username, user.Role)
	uname = user.Username
	return
}

// Register 由管理员创建新用户，用户名唯一，密码 bcrypt 加密，成功后直接签发 JWT。
// 角色默认 viewer，可显式指定 viewer/operator/approver/admin；权限校验复用现有
// manage_users_policy_gates 能力，不开放匿名自助注册。
func Register(ctx context.Context, username, password, requestedRole string) (token, userID, role, uname string, err error) {
	if err = policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return
	}
	if len(password) < 6 {
		err = ErrPasswordTooShort
		return
	}
	if strings.TrimSpace(requestedRole) == "" {
		requestedRole = string(policy.RoleViewer)
	}
	normalizedRole, err := policy.NormalizeRole(requestedRole)
	if err != nil {
		err = ErrInvalidRole
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return
	}
	user, err := dao.CreateUser(ctx, username, string(hashed), string(normalizedRole))
	if err != nil {
		err = fmt.Errorf("用户名已存在或创建失败")
		return
	}
	token, userID, role, err = issueToken(ctx, user.ID, user.Username, user.Role)
	uname = user.Username
	return
}

// ListUsers 返回脱敏用户列表；仅拥有用户管理权限的管理员可读取。
func ListUsers(ctx context.Context) ([]dao.User, error) {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return nil, err
	}
	return dao.ListUsers(ctx)
}

func issueToken(ctx context.Context, id, username, role string) (token, userID, userRole string, err error) {
	normalizedRole, err := policy.NormalizeRole(role)
	if err != nil {
		return "", "", "", err
	}
	exp, _ := g.Cfg().Get(ctx, "auth.jwt.expire_hours")
	expHours := exp.Int()
	if expHours <= 0 {
		expHours = 24
	}
	token, err = auth.Generate(id, username, string(normalizedRole), time.Duration(expHours)*time.Hour)
	userID = id
	userRole = string(normalizedRole)
	return
}
