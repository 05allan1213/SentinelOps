package mysql

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	globalDB *gorm.DB
	dbOnce   sync.Once
	initErr  error
)

type transactionContextKey struct{}

// ContextWithTransaction 将 workflow 已持有的 GORM Transaction 绑定给现有 DAO 调用链。
// 只有事务回调生命周期内可使用返回的 Context，禁止传给 goroutine 或外部调用。
func ContextWithTransaction(ctx context.Context, tx *gorm.DB) (context.Context, error) {
	if ctx == nil || tx == nil {
		return nil, fmt.Errorf("transaction context and GORM transaction are required")
	}
	return context.WithValue(ctx, transactionContextKey{}, tx), nil
}

// HasBoundTransaction 表示当前调用必须只执行可加入 MySQL Transaction 的同步工作。
func HasBoundTransaction(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	tx, ok := ctx.Value(transactionContextKey{}).(*gorm.DB)
	return ok && tx != nil
}

// InitWithDSN 初始化 MySQL 连接并核对 goose Schema 版本；调用方只在
// Secret Resolver 的显式生命周期内提供 DSN。
func InitWithDSN(ctx context.Context, dsn []byte) error {
	dbOnce.Do(func() {
		if len(dsn) == 0 {
			initErr = fmt.Errorf("database DSN is empty")
			return
		}
		db, err := openAndCheckSchema(ctx, string(dsn))
		if err != nil {
			initErr = err
			return
		}
		// 连接池：MaxOpen 限制并发数防打爆 server，MaxIdle 复用减少握手开销，MaxLifetime 防服务端超时强断
		if sqlDB, e := db.DB(); e == nil {
			sqlDB.SetMaxOpenConns(100)
			sqlDB.SetMaxIdleConns(20)
			sqlDB.SetConnMaxLifetime(time.Hour)
			sqlDB.SetConnMaxIdleTime(10 * time.Minute)
		}
		globalDB = db
	})
	return initErr
}

// openAndCheckSchema 打开连接并执行只读版本检查，供启动路径与 contract test 复用。
func openAndCheckSchema(ctx context.Context, rawDSN string) (*gorm.DB, error) {
	// 若 DSN 未指定 loc，追加 loc=Local 确保 Go 与 MySQL 时区一致（autoCreateTime 使用本地时间）。
	if !strings.Contains(rawDSN, "loc=") {
		if strings.Contains(rawDSN, "?") {
			rawDSN += "&loc=Local"
		} else {
			rawDSN += "?loc=Local"
		}
	}
	db, err := gorm.Open(mysql.Open(rawDSN), &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				LogLevel:                  logger.Error,
				IgnoreRecordNotFoundError: true,
			},
		),
	})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	if err := CheckSchemaVersion(ctx, db); err != nil {
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	return db, nil
}

// DB 返回全局 DB 实例，Init 成功后使用
func DB(ctx context.Context) (*gorm.DB, error) {
	if ctx == nil {
		return nil, fmt.Errorf("database context is required")
	}
	if tx, ok := ctx.Value(transactionContextKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx), nil
	}
	if initErr != nil {
		return nil, initErr
	}
	if globalDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return globalDB.WithContext(ctx), nil
}

// RegisterPlugin 注册 GORM plugin（用于 trace 等扩展功能）
func RegisterPlugin(plugin gorm.Plugin) error {
	if globalDB == nil {
		return fmt.Errorf("database not initialized")
	}
	return globalDB.Use(plugin)
}
