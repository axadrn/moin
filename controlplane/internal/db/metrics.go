package db

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

// metricsSchema is the whole of metrics.db: a row per server or app and
// minute, and one per hour. It is history the agents send again every 15
// seconds, so the file has no migrations and no backup: lost, it starts over.
const metricsSchema = `
CREATE TABLE IF NOT EXISTS metrics_minutes (
    target TEXT NOT NULL,
    minute INTEGER NOT NULL,
    cpu INTEGER NOT NULL DEFAULT 0,
    memory INTEGER NOT NULL DEFAULT 0,
    disk INTEGER NOT NULL DEFAULT 0,
    net_rx INTEGER NOT NULL DEFAULT 0,
    net_tx INTEGER NOT NULL DEFAULT 0,
    load INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (target, minute)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_metrics_minutes_minute ON metrics_minutes(minute);
CREATE TABLE IF NOT EXISTS metrics_hours (
    target TEXT NOT NULL,
    hour INTEGER NOT NULL,
    cpu INTEGER NOT NULL DEFAULT 0,
    memory INTEGER NOT NULL DEFAULT 0,
    disk INTEGER NOT NULL DEFAULT 0,
    net_rx INTEGER NOT NULL DEFAULT 0,
    net_tx INTEGER NOT NULL DEFAULT 0,
    load INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (target, hour)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_metrics_hours_hour ON metrics_hours(hour);
`

// MetricsConnection is metrics.db next to the database of connection. A
// database in memory gets its metrics in memory, too.
func MetricsConnection(connection string) string {
	file, _, _ := strings.Cut(strings.TrimPrefix(connection, "file:"), "?")
	if file == "" || file == ":memory:" || strings.Contains(file, "://") {
		return ":memory:"
	}
	return filepath.Join(filepath.Dir(file), "metrics.db") + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
}

// OpenMetrics opens metrics.db and creates its tables.
func OpenMetrics(connection string) (*sqlx.DB, error) {
	database, err := Init(connection)
	if err != nil {
		return nil, fmt.Errorf("metrics database: %w", err)
	}
	if strings.HasPrefix(connection, ":memory:") {
		// Every connection to :memory: is its own database.
		database.SetMaxOpenConns(1)
	}
	if err := CreateMetrics(database); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

// CreateMetrics creates the metrics tables in database if they are missing.
func CreateMetrics(database *sqlx.DB) error {
	if _, err := database.Exec(metricsSchema); err != nil {
		return fmt.Errorf("create metrics tables: %w", err)
	}
	return nil
}
