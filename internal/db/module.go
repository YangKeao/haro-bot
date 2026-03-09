package db

import (
	"github.com/YangKeao/haro-bot/internal/config"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Module provides database connection.
var Module = fx.Module("db",
	fx.Provide(NewDB),
)

// DBParams contains dependencies for creating DB.
type DBParams struct {
	fx.In

	Cfg *config.Config
	Log *zap.Logger
}

// NewDB creates database connection with lifecycle management.
func NewDB(lc fx.Lifecycle, p DBParams) (*gorm.DB, error) {
	conn, err := Open(p.Cfg.TiDBDSN)
	if err != nil {
		return nil, err
	}
	if err := ApplyMigrations(conn, p.Cfg.Memory); err != nil {
		return nil, err
	}
	lc.Append(fx.StopHook(func() error {
		sqlDB, _ := conn.DB()
		return sqlDB.Close()
	}))
	return conn, nil
}
