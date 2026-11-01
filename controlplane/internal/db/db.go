package db

import (
	"fmt"
	"github.com/jmoiron/sqlx"
	"log/slog"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

const (
	databaseDriver  = "sqlite"
	databaseDialect = "sqlite3"
)

func Init(connection string) (*sqlx.DB, error) {
	dir := filepath.Dir(connection)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	database, err := sqlx.Connect(databaseDriver, connection)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	database.SetMaxOpenConns(25)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(5 * time.Minute)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	slog.Info("database connected", "driver", databaseDriver)
	return database, nil
}

func Close(database *sqlx.DB) error {
	if database != nil {
		return database.Close()
	}
	return nil
}
