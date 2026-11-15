package main

import (
	"fmt"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"
)

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS locations (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		employee_id     TEXT    NOT NULL,
		session_id  TEXT,
		platform    TEXT,
		latitude    REAL    NOT NULL,
		longitude   REAL    NOT NULL,
		accuracy    REAL,
		altitude    REAL,
		speed       REAL,
		heading     REAL,
		recorded_at INTEGER NOT NULL,
		sent_at     TEXT,
		received_at INTEGER NOT NULL,
		UNIQUE (employee_id, recorded_at, latitude, longitude)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_locations_user_time ON locations (employee_id, recorded_at DESC)`,
	`CREATE TABLE IF NOT EXISTS employee_uids (
		employee_id TEXT PRIMARY KEY,
		uid         TEXT NOT NULL UNIQUE,
		updated_at  INTEGER NOT NULL
	)`,
}

func migrate(c *gofr.Context) error {
	for _, stmt := range schemaStatements {
		if _, err := c.SQL.ExecContext(c, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	return nil
}

// parseTimeParam accepts epoch milliseconds, epoch seconds or RFC3339 and returns epoch ms.
func parseTimeParam(v string) (int64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}

	var n int64
	if _, err := fmt.Sscanf(v, "%d", &n); err == nil && fmt.Sprint(n) == v {
		return normalizeTimestamp(n), nil
	}

	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return 0, err
	}

	return t.UnixMilli(), nil
}

// normalizeTimestamp converts epoch seconds to ms; zero becomes "now".
func normalizeTimestamp(ts int64) int64 {
	switch {
	case ts <= 0:
		return time.Now().UnixMilli()
	case ts < 100_000_000_000: // seconds
		return ts * 1000
	default:
		return ts
	}
}
