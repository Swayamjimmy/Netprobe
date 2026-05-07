package db

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// Connect opens a PostgreSQL connection and configures the pool
func Connect(databaseURL string) (*sql.DB, error) {
	if databaseURL == "" {
		databaseURL = "postgres://netprobe:netprobe@localhost:5432/netprobe?sslmode=disable"
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	return db, nil
}

// Migrate creates the required tables and indexes
func Migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS diagnostic_runs (
		id SERIAL PRIMARY KEY,
		test_type VARCHAR(50) NOT NULL,
		target VARCHAR(255) NOT NULL,
		result JSONB NOT NULL,
		created_at TIMESTAMP DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS latency_series (
		id SERIAL PRIMARY KEY,
		target VARCHAR(255) NOT NULL,
		avg_rtt DOUBLE PRECISION NOT NULL,
		packet_loss DOUBLE PRECISION NOT NULL,
		created_at TIMESTAMP DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_runs_target ON diagnostic_runs(target);
	CREATE INDEX IF NOT EXISTS idx_runs_created ON diagnostic_runs(created_at);
	CREATE INDEX IF NOT EXISTS idx_series_target ON latency_series(target, created_at);
	`

	_, err := db.Exec(schema)
	return err
}
