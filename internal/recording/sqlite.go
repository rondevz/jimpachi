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
	ID                  string
	Title               string
	StartedAt           time.Time
	Duration            time.Duration
	AudioPath           string
	PendingPromotion    bool
	Interrupted         bool
	AudioMissing        bool
	Transcription       []Segment
	TranscriptionStatus TranscriptionStatus
	TranscriptionError  string
}

// TranscriptionStatus reports the durable state of derived local text.
type TranscriptionStatus string

const (
	TranscriptionPending   TranscriptionStatus = "pending"
	TranscriptionRunning   TranscriptionStatus = "running"
	TranscriptionSucceeded TranscriptionStatus = "succeeded"
	TranscriptionFailed    TranscriptionStatus = "failed"
)

// Segment is a timestamped span of a persisted Transcription.
type Segment struct {
	Start time.Duration
	End   time.Duration
	Text  string
}

// SQLite persists Recording history in a local SQLite database.
type SQLite struct {
	db              *sql.DB
	dataDir         string
	recoveryWarning string
}

// OpenSQLite opens the Recording-history database in dataDir.
func OpenSQLite(ctx context.Context, dataDir string) (*SQLite, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create Jimpachi data directory: %w", err)
	}

	// The SQLite driver applies this URI option to every pooled connection.
	// PRAGMA foreign_keys on one connection would leave others unconstrained.
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(dataDir, "jimpachi.db")+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open Recording history database: %w", err)
	}

	store := &SQLite{db: db, dataDir: dataDir}
	if err := store.initialize(ctx); err != nil {
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
			pending_promotion INTEGER NOT NULL DEFAULT 0,
			interrupted INTEGER NOT NULL DEFAULT 0
			, transcription_status TEXT NOT NULL DEFAULT 'pending'
			, transcription_error TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS recordings_started_at_idx
			ON recordings (started_at_unix_ns DESC);
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS transcription_segments (
			recording_id TEXT NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
			start_ns INTEGER NOT NULL,
			end_ns INTEGER NOT NULL,
			text TEXT NOT NULL,
			PRIMARY KEY (recording_id, start_ns, end_ns)
		);
	`)
	if err != nil {
		return fmt.Errorf("initialize Recording history database: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE recordings ADD COLUMN pending_promotion INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add Recording promotion state: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE recordings ADD COLUMN interrupted INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add Recording interruption state: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE recordings ADD COLUMN transcription_status TEXT NOT NULL DEFAULT 'pending'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add Transcription status: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE recordings ADD COLUMN transcription_error TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add Transcription error: %w", err)
	}

	return nil
}

// SaveTranscriptionStatus records a user-safe processing outcome for a Recording.
func (s *SQLite) SaveTranscriptionStatus(ctx context.Context, id string, status TranscriptionStatus, userError string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE recordings SET transcription_status = ?, transcription_error = ? WHERE id = ?`, status, userError, id)
	if err != nil {
		return fmt.Errorf("save Transcription status for Recording %q: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check Transcription status for Recording %q: %w", id, err)
	}
	if changed == 0 {
		return fmt.Errorf("save Transcription status for Recording %q: not found", id)
	}
	return nil
}

// ResetRunningTranscriptions makes work interrupted by a prior process eligible again.
func (s *SQLite) ResetRunningTranscriptions(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE recordings SET transcription_status = ?, transcription_error = '' WHERE transcription_status = ?`, TranscriptionPending, TranscriptionRunning); err != nil {
		return fmt.Errorf("recover running Transcriptions: %w", err)
	}
	return nil
}

// SaveTranscription replaces the derived Transcription for a completed Recording.
func (s *SQLite) SaveTranscription(ctx context.Context, id string, segments []Segment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Transcription save: %w", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM recordings WHERE id = ?`, id).Scan(&exists); err == sql.ErrNoRows {
		return fmt.Errorf("save Transcription for Recording %q: not found", id)
	} else if err != nil {
		return fmt.Errorf("check Recording %q before Transcription save: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM transcription_segments WHERE recording_id = ?`, id); err != nil {
		return fmt.Errorf("clear Transcription for Recording %q: %w", id, err)
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO transcription_segments (recording_id, start_ns, end_ns, text) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare Transcription save: %w", err)
	}
	defer statement.Close()
	for _, segment := range segments {
		if _, err := statement.ExecContext(ctx, id, segment.Start.Nanoseconds(), segment.End.Nanoseconds(), segment.Text); err != nil {
			return fmt.Errorf("save Transcription segment for Recording %q: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Transcription save: %w", err)
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
		INSERT INTO recordings (id, title, started_at_unix_ns, duration_ns, audio_path, pending_promotion, interrupted, transcription_status, transcription_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			started_at_unix_ns = excluded.started_at_unix_ns,
			duration_ns = excluded.duration_ns,
			audio_path = excluded.audio_path,
			pending_promotion = excluded.pending_promotion,
			interrupted = excluded.interrupted,
			transcription_status = excluded.transcription_status,
			transcription_error = excluded.transcription_error
	`, recording.ID, recording.Title, recording.StartedAt.UnixNano(), recording.Duration.Nanoseconds(), recording.AudioPath, recording.PendingPromotion, recording.Interrupted, transcriptionStatus(recording.TranscriptionStatus), recording.TranscriptionError)
	if err != nil {
		return fmt.Errorf("save Recording %q: %w", recording.ID, err)
	}

	return nil
}

func transcriptionStatus(status TranscriptionStatus) TranscriptionStatus {
	if status == "" {
		return TranscriptionPending
	}
	return status
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

// ReconcileAudio promotes playable staged audio after an interrupted process.
func (s *SQLite) ReconcileAudio(ctx context.Context, playable ...func(string) (bool, error)) error {
	checkPlayable := func(path string) (bool, error) {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0, err
	}
	if len(playable) > 0 {
		checkPlayable = playable[0]
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, audio_path, interrupted FROM recordings WHERE pending_promotion = 1`)
	if err != nil {
		return fmt.Errorf("list Recordings for audio reconciliation: %w", err)
	}
	var missing []string
	var completed []string
	for rows.Next() {
		var id, path string
		var interrupted bool
		if err := rows.Scan(&id, &path, &interrupted); err != nil {
			return fmt.Errorf("read Recording for audio reconciliation: %w", err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			stagedPath := strings.TrimSuffix(path, ".opus") + ".partial.opus"
			if _, stagedErr := os.Stat(stagedPath); stagedErr == nil {
				isPlayable, err := checkPlayable(stagedPath)
				if err != nil {
					// Keep both artifacts so a later start can retry when ffprobe is available.
					s.recoveryWarning = fmt.Sprintf("Could not verify interrupted Recording %q; it will be retried next start: %v", id, err)
					continue
				}
				if !isPlayable {
					missing = append(missing, id)
					continue
				}
				// A staged file survives only when a decoder can read it; its row was
				// deliberately marked interrupted before capture began for crash recovery.
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

// RecoveryWarning reports a nonfatal interrupted-Recording recovery condition.
func (s *SQLite) RecoveryWarning() string { return s.recoveryWarning }

func (s *SQLite) completePromotion(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE recordings SET pending_promotion = 0 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("complete Recording promotion %q: %w", id, err)
	}
	return nil
}

// History returns Recordings ordered from newest to oldest.
func (s *SQLite) History(ctx context.Context) ([]Recording, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, started_at_unix_ns, duration_ns, audio_path, pending_promotion, interrupted, transcription_status, transcription_error
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
		if err := rows.Scan(&recording.ID, &recording.Title, &startedAt, &duration, &recording.AudioPath, &recording.PendingPromotion, &recording.Interrupted, &recording.TranscriptionStatus, &recording.TranscriptionError); err != nil {
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

// Recording returns one Recording and its full timestamped Transcription.
func (s *SQLite) Recording(ctx context.Context, id string) (Recording, error) {
	var result Recording
	var startedAt, duration int64
	err := s.db.QueryRowContext(ctx, `SELECT id, title, started_at_unix_ns, duration_ns, audio_path, pending_promotion, interrupted, transcription_status, transcription_error FROM recordings WHERE id = ?`, id).Scan(&result.ID, &result.Title, &startedAt, &duration, &result.AudioPath, &result.PendingPromotion, &result.Interrupted, &result.TranscriptionStatus, &result.TranscriptionError)
	if err == sql.ErrNoRows {
		return Recording{}, fmt.Errorf("load Recording %q: not found", id)
	}
	if err != nil {
		return Recording{}, fmt.Errorf("load Recording %q: %w", id, err)
	}
	result.StartedAt = time.Unix(0, startedAt).UTC()
	result.Duration = time.Duration(duration)
	if _, err := os.Stat(result.AudioPath); os.IsNotExist(err) {
		result.AudioMissing = true
	} else if err != nil {
		return Recording{}, fmt.Errorf("stat Recording audio %q: %w", result.AudioPath, err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT start_ns, end_ns, text FROM transcription_segments WHERE recording_id = ? ORDER BY start_ns, end_ns`, id)
	if err != nil {
		return Recording{}, fmt.Errorf("load Transcription for Recording %q: %w", id, err)
	}
	defer rows.Close()
	for rows.Next() {
		var start, end int64
		var segment Segment
		if err := rows.Scan(&start, &end, &segment.Text); err != nil {
			return Recording{}, fmt.Errorf("read Transcription for Recording %q: %w", id, err)
		}
		segment.Start, segment.End = time.Duration(start), time.Duration(end)
		result.Transcription = append(result.Transcription, segment)
	}
	if err := rows.Err(); err != nil {
		return Recording{}, fmt.Errorf("iterate Transcription for Recording %q: %w", id, err)
	}
	return result, nil
}

// Close releases the local database connection.
func (s *SQLite) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close Recording history database: %w", err)
	}

	return nil
}
