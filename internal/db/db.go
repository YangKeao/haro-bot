package db

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Open(dsn string) (*gorm.DB, error) {
	gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}
	return gdb, nil
}

func ApplyMigrations(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return applySQLMigrations(sqlDB)
}

func applySQLMigrations(db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		path := "migrations/" + entry.Name()
		data, err := migrationsFS.ReadFile(path)
		if err != nil {
			return err
		}
		statements := splitSQLStatements(string(data))
		for _, stmt := range statements {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("apply %s: %w", entry.Name(), err)
			}
		}
	}
	return nil
}

func splitSQLStatements(input string) []string {
	var stmts []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	inBacktick := false
	escape := false
	for _, r := range input {
		if escape {
			escape = false
			b.WriteRune(r)
			continue
		}
		if r == '\\' && (inSingle || inDouble) {
			escape = true
			b.WriteRune(r)
			continue
		}
		if r == '\'' && !inDouble && !inBacktick {
			inSingle = !inSingle
		} else if r == '"' && !inSingle && !inBacktick {
			inDouble = !inDouble
		} else if r == '`' && !inSingle && !inDouble {
			inBacktick = !inBacktick
		}
		if r == ';' && !inSingle && !inDouble && !inBacktick {
			stmt := strings.TrimSpace(b.String())
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			b.Reset()
			continue
		}
		b.WriteRune(r)
	}
	if tail := strings.TrimSpace(b.String()); tail != "" {
		stmts = append(stmts, tail)
	}
	return stmts
}
