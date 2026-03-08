//go:build integration

package db

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestApplyMigrationsSetsSchemaVersion(t *testing.T) {
	gdb, cleanup := newTestDB(t)
	t.Cleanup(cleanup)
	if err := ApplyMigrations(gdb, config.MemoryConfig{Enabled: false}); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ver, err := getSchemaVersion(gdb)
	if err != nil {
		t.Fatalf("get schema version: %v", err)
	}
	if ver != currentSchemaVersion {
		t.Fatalf("expected version %d, got %d", currentSchemaVersion, ver)
	}
}

func TestApplyMigrationsIdempotent(t *testing.T) {
	gdb, cleanup := newTestDB(t)
	t.Cleanup(cleanup)
	if err := ApplyMigrations(gdb, config.MemoryConfig{Enabled: false}); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if err := ApplyMigrations(gdb, config.MemoryConfig{Enabled: false}); err != nil {
		t.Fatalf("apply migrations second time: %v", err)
	}
	ver, err := getSchemaVersion(gdb)
	if err != nil {
		t.Fatalf("get schema version: %v", err)
	}
	if ver != currentSchemaVersion {
		t.Fatalf("expected version %d, got %d", currentSchemaVersion, ver)
	}
}

func newTestDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	dsn := os.Getenv("TIDB_DSN")
	if dsn == "" {
		t.Skip("TIDB_DSN not set")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	baseName := parsed.DBName
	if baseName == "" {
		baseName = "haro_bot_test"
	}
	testName := fmt.Sprintf("%s_%d_%d", baseName, time.Now().UnixNano(), rand.Intn(10000))
	testName = sanitizeDBName(testName)
	adminCfg := *parsed
	adminCfg.DBName = ""
	adminDSN := adminCfg.FormatDSN()
	adminDB, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatalf("admin open: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE `" + testName + "`"); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create database: %v", err)
	}
	parsed.DBName = testName
	testDSN := parsed.FormatDSN()
	gdb, err := gorm.Open(gormmysql.Open(testDSN), &gorm.Config{})
	if err != nil {
		_, _ = adminDB.Exec("DROP DATABASE `" + testName + "`")
		_ = adminDB.Close()
		t.Fatalf("gorm open: %v", err)
	}
	cleanup := func() {
		sqlDB, _ := gdb.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		_, _ = adminDB.Exec("DROP DATABASE `" + testName + "`")
		_ = adminDB.Close()
	}
	return gdb, cleanup
}

func sanitizeDBName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
