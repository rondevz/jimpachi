package recording

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Recording is the persisted metadata for one system-output capture.
type Recording struct {
	ID               string
	Title            string
	StartedAt        time.Time
	Duration         time.Duration
	AudioPath        string
	PendingPromotion bool
	AudioMissing     bool
}

// SQLite persists Recording history in a local SQLite database.
type SQLite struct {
	db      *sql.DB
	dataDir string
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

	store := &SQLite{db: db, dataDir: dataDir}
	if err := store.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.ReconcileAudio(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

// DataDir returns the directory containing local Recording data.
func (s *SQLite) DataDir() string { return s.dataDir }

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
			audio_path TEXT NOT NULL,
			pending_promotion INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS recordings_started_at_idx
			ON recordings (started_at_unix_ns DESC);
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("initialize Recording history database: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE recordings ADD COLUMN pending_promotion INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add Recording promotion state: %w", err)
	}

	return nil
}

// Setting returns a locally persisted application setting.
func (s *SQLite) Setting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load setting %q: %w", key, err)
	}

	return value, true, nil
}

// SaveSetting stores a locally persisted application setting.
func (s *SQLite) SaveSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	if err != nil {
		return fmt.Errorf("save setting %q: %w", key, err)
	}

	return nil
}

// SaveSettings stores related application settings atomically.
func (s *SQLite) SaveSettings(ctx context.Context, settings map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings transaction: %w", err)
	}
	defer tx.Rollback()

	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`)
	if err != nil {
		return fmt.Errorf("prepare settings save: %w", err)
	}
	defer statement.Close()

	for key, value := range settings {
		if _, err := statement.ExecContext(ctx, key, value); err != nil {
			return fmt.Errorf("save setting %q: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings transaction: %w", err)
	}

	return nil
}

// Save adds or updates a Recording in local history.
func (s *SQLite) Save(ctx context.Context, recording Recording) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO recordings (id, title, started_at_unix_ns, duration_ns, audio_path, pending_promotion)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			started_at_unix_ns = excluded.started_at_unix_ns,
			duration_ns = excluded.duration_ns,
			audio_path = excluded.audio_path,
			pending_promotion = excluded.pending_promotion
	`, recording.ID, recording.Title, recording.StartedAt.UnixNano(), recording.Duration.Nanoseconds(), recording.AudioPath, recording.PendingPromotion)
	if err != nil {
		return fmt.Errorf("save Recording %q: %w", recording.ID, err)
	}

	return nil
}

// Rename updates a Recording title without altering its capture metadata.
func (s *SQLite) Rename(ctx context.Context, id, title string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE recordings SET title = ? WHERE id = ?`, title, id)
	if err != nil {
		return fmt.Errorf("rename Recording %q: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check renamed Recording %q: %w", id, err)
	}
	if changed == 0 {
		return fmt.Errorf("rename Recording %q: not found", id)
	}
	return nil
}

// Delete removes a Recording whose completed audio could not be promoted.
func (s *SQLite) Delete(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM recordings WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete Recording %q: %w", id, err)
	}
	return nil
}

// ReconcileAudio removes metadata left by an interrupted promotion before a
// final Recording audio file could be made durable.
func (s *SQLite) ReconcileAudio(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, audio_path, pending_promotion FROM recordings WHERE pending_promotion = 1`)
	if err != nil {
		return fmt.Errorf("list Recordings for audio reconciliation: %w", err)
	}
	var missing []string
	var completed []string
	for rows.Next() {
		var id, path string
		var pending bool
		if err := rows.Scan(&id, &path, &pending); err != nil {
			return fmt.Errorf("read Recording for audio reconciliation: %w", err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			stagedPath := strings.TrimSuffix(path, ".opus") + ".partial.opus"
			if _, stagedErr := os.Stat(stagedPath); stagedErr == nil {
				if err := os.Rename(stagedPath, path); err != nil {
					return fmt.Errorf("promote staged Recording audio %q: %w", stagedPath, err)
				}
				completed = append(completed, id)
			} else if os.IsNotExist(stagedErr) {
				missing = append(missing, id)
			} else {
				return fmt.Errorf("stat staged Recording audio %q: %w", stagedPath, stagedErr)
			}
		} else if err != nil {
			return fmt.Errorf("stat Recording audio %q: %w", path, err)
		} else {
			completed = append(completed, id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate Recordings for audio reconciliation: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close Recording reconciliation rows: %w", err)
	}
	for _, id := range completed {
		if err := s.completePromotion(ctx, id); err != nil {
			return err
		}
	}
	for _, id := range missing {
		if err := s.Delete(ctx, id); err != nil {
			return fmt.Errorf("remove interrupted Recording: %w", err)
		}
	}
	return nil
}

func (s *SQLite) completePromotion(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE recordings SET pending_promotion = 0 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("complete Recording promotion %q: %w", id, err)
	}
	return nil
}

// History returns Recordings ordered from newest to oldest.
func (s *SQLite) History(ctx context.Context) ([]Recording, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, started_at_unix_ns, duration_ns, audio_path, pending_promotion
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
		if err := rows.Scan(&recording.ID, &recording.Title, &startedAt, &duration, &recording.AudioPath, &recording.PendingPromotion); err != nil {
			return nil, fmt.Errorf("read Recording history: %w", err)
		}

		recording.StartedAt = time.Unix(0, startedAt).UTC()
		recording.Duration = time.Duration(duration)
		if _, err := os.Stat(recording.AudioPath); os.IsNotExist(err) {
			recording.AudioMissing = true
		} else if err != nil {
			return nil, fmt.Errorf("stat Recording audio %q: %w", recording.AudioPath, err)
		}
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
