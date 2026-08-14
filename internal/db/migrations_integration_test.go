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

	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestApplyMigrationsSetsSchemaVersion(t *testing.T) {
	gdb, cleanup := newTestDB(t)
	t.Cleanup(cleanup)
	if err := gdb.Exec("CREATE TABLE tool_audit (id BIGINT PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create legacy tool_audit table: %v", err)
	}
	if err := ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ver, err := getSchemaVersion(gdb)
	if err != nil {
		t.Fatalf("get schema version: %v", err)
	}
	if ver != currentSchemaVersion {
		t.Fatalf("expected version %d, got %d", currentSchemaVersion, ver)
	}
	var legacyTableCount int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'tool_audit'`).Scan(&legacyTableCount).Error; err != nil {
		t.Fatalf("query legacy tool_audit table: %v", err)
	}
	if legacyTableCount != 0 {
		t.Fatalf("expected legacy tool_audit table to be dropped")
	}
	assertDeadSchemaRemoved(t, gdb)
}

func TestApplyMigrationsIdempotent(t *testing.T) {
	gdb, cleanup := newTestDB(t)
	t.Cleanup(cleanup)
	if err := ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if err := ApplyMigrations(gdb); err != nil {
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

func TestApplyMigrationsDropsLegacyMemoryTablesFromVersion12(t *testing.T) {
	gdb, cleanup := newTestDB(t)
	t.Cleanup(cleanup)
	if err := gdb.AutoMigrate(&schemaMigration{}); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}
	if err := setSchemaVersion(gdb, 12); err != nil {
		t.Fatalf("set schema version: %v", err)
	}
	createLegacyDeadSchema(t, gdb)
	for _, table := range []string{"memory_items", "memories"} {
		if err := gdb.Exec("CREATE TABLE " + table + " (id BIGINT PRIMARY KEY)").Error; err != nil {
			t.Fatalf("create legacy %s table: %v", table, err)
		}
	}

	if err := ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	for _, table := range []string{"memory_items", "memories"} {
		var count int64
		if err := gdb.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&count).Error; err != nil {
			t.Fatalf("query legacy %s table: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected legacy %s table to be dropped", table)
		}
	}
	assertDeadSchemaRemoved(t, gdb)
}

func TestApplyMigrationsDropsDeadSchemaFromVersion13(t *testing.T) {
	gdb, cleanup := newTestDB(t)
	t.Cleanup(cleanup)
	if err := gdb.AutoMigrate(&schemaMigration{}); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}
	if err := setSchemaVersion(gdb, 13); err != nil {
		t.Fatalf("set schema version: %v", err)
	}
	createLegacyDeadSchema(t, gdb)

	if err := ApplyMigrations(gdb); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	assertDeadSchemaRemoved(t, gdb)
}

func createLegacyDeadSchema(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE users (id BIGINT PRIMARY KEY, profile_json JSON)`,
		`CREATE TABLE sessions (id BIGINT PRIMARY KEY, summary TEXT, status VARCHAR(16))`,
		`CREATE TABLE session_summaries (id BIGINT PRIMARY KEY, source_entry_ids JSON)`,
		`CREATE TABLE app_config (id BIGINT PRIMARY KEY)`,
	}
	for _, stmt := range statements {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("create legacy schema with %q: %v", stmt, err)
		}
	}
}

func assertDeadSchemaRemoved(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	var tableCount int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'app_config'`).Scan(&tableCount).Error; err != nil {
		t.Fatalf("query app_config table: %v", err)
	}
	if tableCount != 0 {
		t.Fatal("expected app_config table to be dropped")
	}
	columns := []struct {
		table  string
		column string
	}{
		{table: "users", column: "profile_json"},
		{table: "sessions", column: "summary"},
		{table: "sessions", column: "status"},
		{table: "session_summaries", column: "source_entry_ids"},
	}
	for _, item := range columns {
		var count int64
		if err := gdb.Raw(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, item.table, item.column).Scan(&count).Error; err != nil {
			t.Fatalf("query %s.%s column: %v", item.table, item.column, err)
		}
		if count != 0 {
			t.Fatalf("expected %s.%s column to be dropped", item.table, item.column)
		}
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
