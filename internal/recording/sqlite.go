package recording

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Recording is the persisted metadata for one system-output capture.
type Recording struct {
	ID        string
	Title     string
	StartedAt time.Time
	Duration  time.Duration
	AudioPath string
}

// SQLite persists Recording history in a local SQLite database.
type SQLite struct {
	db *sql.DB
}

// OpenSQLite opens the Recording-history database in dataDir.
func OpenSQLite(ctx context.Context, dataDir string) (*SQLite, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create Jimpachi data directory: %w", err)
	}

	db, err := sql.Open("sqlite3", filepath.Join(dataDir, "jimpachi.db"))
	if err != nil {
		return nil, fmt.Errorf("open Recording history database: %w", err)
	}

	store := &SQLite{db: db}
	if err := store.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLite) initialize(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to Recording history database: %w", err)
	}

	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS recordings (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			started_at_unix_ns INTEGER NOT NULL,
			duration_ns INTEGER NOT NULL,
			audio_path TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS recordings_started_at_idx
			ON recordings (started_at_unix_ns DESC);
	`)
	if err != nil {
		return fmt.Errorf("initialize Recording history database: %w", err)
	}

	return nil
}

// Save adds or updates a Recording in local history.
func (s *SQLite) Save(ctx context.Context, recording Recording) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO recordings (id, title, started_at_unix_ns, duration_ns, audio_path)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			started_at_unix_ns = excluded.started_at_unix_ns,
			duration_ns = excluded.duration_ns,
			audio_path = excluded.audio_path
	`, recording.ID, recording.Title, recording.StartedAt.UnixNano(), recording.Duration.Nanoseconds(), recording.AudioPath)
	if err != nil {
		return fmt.Errorf("save Recording %q: %w", recording.ID, err)
	}

	return nil
}

// History returns Recordings ordered from newest to oldest.
func (s *SQLite) History(ctx context.Context) ([]Recording, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, started_at_unix_ns, duration_ns, audio_path
		FROM recordings
		ORDER BY started_at_unix_ns DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("load Recording history: %w", err)
	}
	defer rows.Close()

	var history []Recording
	for rows.Next() {
		var recording Recording
		var startedAt, duration int64
		if err := rows.Scan(&recording.ID, &recording.Title, &startedAt, &duration, &recording.AudioPath); err != nil {
			return nil, fmt.Errorf("read Recording history: %w", err)
		}

		recording.StartedAt = time.Unix(0, startedAt).UTC()
		recording.Duration = time.Duration(duration)
		history = append(history, recording)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Recording history: %w", err)
	}

	return history, nil
}

// Close releases the local database connection.
func (s *SQLite) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close Recording history database: %w", err)
	}

	return nil
}
