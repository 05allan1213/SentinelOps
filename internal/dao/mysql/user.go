package mysql

import (
	"context"

	"github.com/google/uuid"
)

// FindUserByUsername 按用户名查询用户，用于登录鉴权。
// 用户不存在时返回 gorm.ErrRecordNotFound。
func FindUserByUsername(ctx context.Context, username string) (*User, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var user User
	if err = db.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// CreateUser 创建新用户，role 固定为 user。
func CreateUser(ctx context.Context, username, hashedPassword string) (*User, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	user := &User{
		ID:       uuid.NewString(),
		Username: username,
		Password: hashedPassword,
		Role:     "user",
	}
	if err = db.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}
