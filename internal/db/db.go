package db

import (
	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func Open(dsn string) (*gorm.DB, error) {
	log := logging.L().Named("db")
	gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Error("db open failed", zap.Error(err))
		return nil, err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		log.Error("db get sql handle failed", zap.Error(err))
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		log.Error("db ping failed", zap.Error(err))
		return nil, err
	}
	log.Info("db connection ready")
	return gdb, nil
}

func ApplyMigrations(db *gorm.DB) error {
	return applyMigrations(db)
}
