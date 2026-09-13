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

// CreateUser 创建新用户；role 由调用方完成策略归一化后传入。
func CreateUser(ctx context.Context, username, hashedPassword, role string) (*User, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	user := &User{
		ID:       uuid.NewString(),
		Username: username,
		Password: hashedPassword,
		Role:     role,
	}
	if err = db.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// ListUsers 返回全部用户，按创建时间升序，供管理员用户管理页使用。
func ListUsers(ctx context.Context) ([]User, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var users []User
	if err = db.Order("created_at ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}
