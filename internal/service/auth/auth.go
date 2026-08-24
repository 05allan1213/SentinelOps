// Package authsvc 提供用户认证业务逻辑。
// 职责：校验密码 → 签发 JWT，不含 HTTP 层细节。
package authsvc

import (
	"context"
	"fmt"
	"time"

	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/utility/auth"

	"github.com/gogf/gf/v2/frame/g"
	"golang.org/x/crypto/bcrypt"
)

// Login 校验用户名/密码，成功后签发 JWT Token。
// 密码错误与用户不存在使用相同错误消息，防止枚举攻击。
// 过期时长从配置 auth.jwt.expire_hours 读取，未配置时默认 24 小时。
func Login(ctx context.Context, username, password string) (token, userID, role, uname string, err error) {
	user, err := dao.FindUserByUsername(ctx, username)
	if err != nil {
		return
	}
	if err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return
	}
	token, userID, role, err = issueToken(ctx, user.ID, user.Username, user.Role)
	uname = user.Username
	return
}

// Register 注册新用户，用户名唯一，密码 bcrypt 加密，成功后直接签发 JWT。
func Register(ctx context.Context, username, password string) (token, userID, role, uname string, err error) {
	if len(password) < 6 {
		err = fmt.Errorf("密码长度不能少于 6 位")
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return
	}
	user, err := dao.CreateUser(ctx, username, string(hashed))
	if err != nil {
		err = fmt.Errorf("用户名已存在或创建失败")
		return
	}
	token, userID, role, err = issueToken(ctx, user.ID, user.Username, user.Role)
	uname = user.Username
	return
}

func issueToken(ctx context.Context, id, username, role string) (token, userID, userRole string, err error) {
	exp, _ := g.Cfg().Get(ctx, "auth.jwt.expire_hours")
	expHours := exp.Int()
	if expHours <= 0 {
		expHours = 24
	}
	token, err = auth.Generate(id, username, role, time.Duration(expHours)*time.Hour)
	userID = id
	userRole = role
	return
}
