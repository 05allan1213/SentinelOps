package mysql

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	driver "github.com/go-sql-driver/mysql"
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
	normalizedDSN, err := normalizeApplicationDSN(rawDSN)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(mysql.Open(normalizedDSN), &gorm.Config{
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

// normalizeApplicationDSN 将应用与 MySQL 连接统一到 UTC，避免 DATETIME 在不同会话时区间发生偏移。
func normalizeApplicationDSN(rawDSN string) (string, error) {
	cfg, err := driver.ParseDSN(rawDSN)
	if err != nil {
		return "", fmt.Errorf("parse mysql DSN: %w", err)
	}
	if !cfg.ParseTime {
		return "", fmt.Errorf("mysql DSN must enable parseTime")
	}
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	// go-sql-driver/mysql 会在每条新连接上用 SET 应用 Params；显式固定字符集，
	// 避免连接继承服务端 latin1 后写入中文错误、Prompt 或工具结果时失败。
	cfg.Params["charset"] = "utf8mb4"
	// 数字 offset 不依赖 MySQL 时区表。
	cfg.Params["time_zone"] = "'+00:00'"
	return cfg.FormatDSN(), nil
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
