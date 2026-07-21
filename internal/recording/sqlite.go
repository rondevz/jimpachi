package recording

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Recording is the persisted metadata for one system-output capture.
type Recording struct {
	ID                           string
	Title                        string
	StartedAt                    time.Time
	Duration                     time.Duration
	AudioPath                    string
	PendingPromotion             bool
	Interrupted                  bool
	AudioMissing                 bool
	Transcription                []Segment
	TranscriptionStatus          TranscriptionStatus
	TranscriptionFailureCategory ProcessingFailureCategory
	TranscriptionError           string
	TranscriptionQueuedAt        time.Time
	TranscriptionAttempt         uint64
	Summary                      Summary
	SummaryStatus                TranscriptionStatus
	SummaryFailureCategory       ProcessingFailureCategory
	SummaryError                 string
	SummaryQueuedAt              time.Time
	SummaryAttempt               uint64
	SummaryProgress              int
}

// Summary is the structured derived quick view of a Transcription.
type Summary struct {
	Title, Overview                                                string
	Agreements, Suggestions, ActionItems, Deadlines, OpenQuestions []string
}

// TranscriptionStatus reports the durable state of derived local text.
type TranscriptionStatus string

const (
	TranscriptionNotQueued TranscriptionStatus = "not_queued"
	TranscriptionPending   TranscriptionStatus = "pending"
	TranscriptionRunning   TranscriptionStatus = "running"
	TranscriptionSucceeded TranscriptionStatus = "succeeded"
	TranscriptionFailed    TranscriptionStatus = "failed"
	TranscriptionCancelled TranscriptionStatus = "cancelled"
)

// ProcessingFailureCategory is a stable, user-safe reason for a failed attempt.
type ProcessingFailureCategory string

const (
	ProcessingFailureConfiguration ProcessingFailureCategory = "configuration"
	ProcessingFailureExecution     ProcessingFailureCategory = "execution"
	ProcessingFailureCancelled     ProcessingFailureCategory = "cancelled"
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
			, transcription_status TEXT NOT NULL DEFAULT 'not_queued'
			, transcription_failure_category TEXT NOT NULL DEFAULT ''
			, transcription_error TEXT NOT NULL DEFAULT ''
			, transcription_queued_at_unix_ns INTEGER NOT NULL DEFAULT 0
			, transcription_attempt INTEGER NOT NULL DEFAULT 0
			, summary_title TEXT NOT NULL DEFAULT ''
			, summary_overview TEXT NOT NULL DEFAULT ''
			, summary_agreements TEXT NOT NULL DEFAULT '[]'
			, summary_suggestions TEXT NOT NULL DEFAULT '[]'
			, summary_action_items TEXT NOT NULL DEFAULT '[]'
			, summary_deadlines TEXT NOT NULL DEFAULT '[]'
			, summary_open_questions TEXT NOT NULL DEFAULT '[]'
			, summary_status TEXT NOT NULL DEFAULT 'not_queued'
			, summary_failure_category TEXT NOT NULL DEFAULT ''
			, summary_error TEXT NOT NULL DEFAULT ''
			, summary_queued_at_unix_ns INTEGER NOT NULL DEFAULT 0
			, summary_attempt INTEGER NOT NULL DEFAULT 0
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
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE recordings ADD COLUMN transcription_failure_category TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add Transcription failure category: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE recordings ADD COLUMN transcription_queued_at_unix_ns INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add Transcription queue time: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE recordings ADD COLUMN transcription_attempt INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add Transcription attempt: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE recordings SET transcription_failure_category = ? WHERE transcription_status = ? AND transcription_failure_category = ''`, ProcessingFailureExecution, TranscriptionFailed); err != nil {
		return fmt.Errorf("migrate Transcription failure category: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE recordings SET transcription_status = ? WHERE transcription_status = ? AND transcription_queued_at_unix_ns = 0`, TranscriptionNotQueued, TranscriptionPending); err != nil {
		return fmt.Errorf("migrate unqueued Transcriptions: %w", err)
	}
	for _, column := range []string{"summary_title TEXT NOT NULL DEFAULT ''", "summary_overview TEXT NOT NULL DEFAULT ''", "summary_agreements TEXT NOT NULL DEFAULT '[]'", "summary_suggestions TEXT NOT NULL DEFAULT '[]'", "summary_action_items TEXT NOT NULL DEFAULT '[]'", "summary_deadlines TEXT NOT NULL DEFAULT '[]'", "summary_open_questions TEXT NOT NULL DEFAULT '[]'", "summary_status TEXT NOT NULL DEFAULT 'not_queued'", "summary_failure_category TEXT NOT NULL DEFAULT ''", "summary_error TEXT NOT NULL DEFAULT ''", "summary_queued_at_unix_ns INTEGER NOT NULL DEFAULT 0", "summary_attempt INTEGER NOT NULL DEFAULT 0"} {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE recordings ADD COLUMN `+column); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("add Summary state: %w", err)
		}
	}

	return nil
}

// QueueSummary makes a successful Transcription eligible for local summary generation.
func (s *SQLite) QueueSummary(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE recordings SET summary_status=?, summary_failure_category='', summary_error='', summary_queued_at_unix_ns=? WHERE id=? AND transcription_status=? AND summary_status IN (?, ?, ?, ?)`, TranscriptionPending, time.Now().UnixNano(), id, TranscriptionSucceeded, TranscriptionNotQueued, TranscriptionSucceeded, TranscriptionFailed, TranscriptionCancelled)
	if err != nil {
		return fmt.Errorf("queue Summary for Recording %q: %w", id, err)
	}
	return nil
}

// CancelQueuedSummary conditionally cancels Summary work that has not been claimed.
func (s *SQLite) CancelQueuedSummary(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE recordings SET summary_status=?,summary_failure_category=?,summary_error='',summary_queued_at_unix_ns=0 WHERE id=? AND summary_status=?`, TranscriptionCancelled, ProcessingFailureCancelled, id, TranscriptionPending)
	if err != nil {
		return false, fmt.Errorf("cancel queued Summary for Recording %q: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check cancelled queued Summary for Recording %q: %w", id, err)
	}
	return changed == 1, nil
}

// ClaimNextPendingSummary atomically reserves the oldest pending Summary.
func (s *SQLite) ClaimNextPendingSummary(ctx context.Context) (*Recording, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin Summary claim: %w", err)
	}
	defer tx.Rollback()
	var r Recording
	var started, duration, queued int64
	err = tx.QueryRowContext(ctx, `SELECT id,title,started_at_unix_ns,duration_ns,audio_path,summary_attempt FROM recordings WHERE summary_status=? AND transcription_status=? ORDER BY summary_queued_at_unix_ns,started_at_unix_ns,id LIMIT 1`, TranscriptionPending, TranscriptionSucceeded).Scan(&r.ID, &r.Title, &started, &duration, &r.AudioPath, &r.SummaryAttempt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select pending Summary: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE recordings SET summary_status=?,summary_attempt=summary_attempt+1,summary_failure_category='',summary_error='' WHERE id=? AND summary_status=? AND summary_attempt=?`, TranscriptionRunning, r.ID, TranscriptionPending, r.SummaryAttempt)
	if err != nil {
		return nil, fmt.Errorf("claim Summary: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	r.StartedAt = time.Unix(0, started).UTC()
	r.Duration = time.Duration(duration)
	r.SummaryStatus = TranscriptionRunning
	r.SummaryAttempt++
	_ = queued
	return &r, nil
}

// CompleteSummaryAttempt persists a Summary and applies its proposed title only while the default title remains unchanged.
func (s *SQLite) CompleteSummaryAttempt(ctx context.Context, id string, attempt uint64, summary Summary, defaultTitle string) (bool, error) {
	encode := func(v []string) string { b, _ := json.Marshal(v); return string(b) }
	result, err := s.db.ExecContext(ctx, `UPDATE recordings SET summary_title=?,summary_overview=?,summary_agreements=?,summary_suggestions=?,summary_action_items=?,summary_deadlines=?,summary_open_questions=?,summary_status=?,summary_failure_category='',summary_error='',summary_queued_at_unix_ns=0,title=CASE WHEN title=? THEN ? ELSE title END WHERE id=? AND summary_status=? AND summary_attempt=?`, summary.Title, summary.Overview, encode(summary.Agreements), encode(summary.Suggestions), encode(summary.ActionItems), encode(summary.Deadlines), encode(summary.OpenQuestions), TranscriptionSucceeded, defaultTitle, summary.Title, id, TranscriptionRunning, attempt)
	if err != nil {
		return false, fmt.Errorf("complete Summary for Recording %q: %w", id, err)
	}
	n, _ := result.RowsAffected()
	return n == 1, nil
}

// TransitionSummaryAttempt conditionally finishes one claimed Summary attempt.
func (s *SQLite) TransitionSummaryAttempt(ctx context.Context, id string, attempt uint64, status TranscriptionStatus, category ProcessingFailureCategory, detail string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE recordings SET summary_status=?,summary_failure_category=?,summary_error=?,summary_queued_at_unix_ns=? WHERE id=? AND summary_status=? AND summary_attempt=?`, status, category, detail, func() int64 {
		if status == TranscriptionPending {
			return time.Now().UnixNano()
		}
		return 0
	}(), id, TranscriptionRunning, attempt)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n == 1, nil
}

// ResetRunningSummaries makes interrupted Summary work eligible again.
func (s *SQLite) ResetRunningSummaries(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE recordings SET summary_status=?,summary_failure_category='',summary_error='',summary_queued_at_unix_ns=? WHERE summary_status=?`, TranscriptionPending, time.Now().UnixNano(), TranscriptionRunning)
	return err
}

// SaveTranscriptionStatus records a user-safe processing outcome for a Recording.
func (s *SQLite) SaveTranscriptionStatus(ctx context.Context, id string, status TranscriptionStatus, userError string) error {
	return s.SaveTranscriptionOutcome(ctx, id, status, "", userError)
}

// SaveTranscriptionOutcome records a durable user-safe processing outcome.
func (s *SQLite) SaveTranscriptionOutcome(ctx context.Context, id string, status TranscriptionStatus, category ProcessingFailureCategory, userDetail string) error {
	queuedAt := int64(0)
	if status == TranscriptionPending {
		queuedAt = time.Now().UnixNano()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE recordings SET transcription_status = ?, transcription_failure_category = ?, transcription_error = ?, transcription_queued_at_unix_ns = ? WHERE id = ?`, status, category, userDetail, queuedAt, id)
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

// QueueTranscription makes a completed Recording eligible for one worker attempt.
func (s *SQLite) QueueTranscription(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE recordings SET transcription_status = ?, transcription_failure_category = '', transcription_error = '', transcription_queued_at_unix_ns = ? WHERE id = ? AND transcription_status IN (?, ?, ?, ?)`, TranscriptionPending, time.Now().UnixNano(), id, TranscriptionNotQueued, TranscriptionSucceeded, TranscriptionFailed, TranscriptionCancelled)
	if err != nil {
		return fmt.Errorf("queue Transcription for Recording %q: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check queued Transcription for Recording %q: %w", id, err)
	}
	if changed == 0 {
		return nil
	}
	return nil
}

// CancelQueuedTranscription conditionally cancels work that has not been claimed.
func (s *SQLite) CancelQueuedTranscription(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE recordings SET transcription_status = ?, transcription_failure_category = ?, transcription_error = '', transcription_queued_at_unix_ns = 0 WHERE id = ? AND transcription_status = ?`, TranscriptionCancelled, ProcessingFailureCancelled, id, TranscriptionPending)
	if err != nil {
		return false, fmt.Errorf("cancel queued Transcription for Recording %q: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check cancelled queued Transcription for Recording %q: %w", id, err)
	}
	return changed == 1, nil
}

// TransitionTranscriptionAttempt conditionally finishes one claimed attempt.
func (s *SQLite) TransitionTranscriptionAttempt(ctx context.Context, id string, attempt uint64, status TranscriptionStatus, category ProcessingFailureCategory, userDetail string) (bool, error) {
	return s.transitionAttempt(ctx, id, attempt, TranscriptionRunning, status, category, userDetail)
}

func (s *SQLite) transitionAttempt(ctx context.Context, id string, attempt uint64, from, to TranscriptionStatus, category ProcessingFailureCategory, detail string) (bool, error) {
	queuedAt := int64(0)
	if to == TranscriptionPending {
		queuedAt = time.Now().UnixNano()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE recordings SET transcription_status = ?, transcription_failure_category = ?, transcription_error = ?, transcription_queued_at_unix_ns = ? WHERE id = ? AND transcription_status = ? AND transcription_attempt = ?`, to, category, detail, queuedAt, id, from, attempt)
	if err != nil {
		return false, fmt.Errorf("transition Transcription for Recording %q: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check Transcription transition for Recording %q: %w", id, err)
	}
	return changed == 1, nil
}

// ClaimNextPendingTranscription atomically reserves the oldest pending Recording.
func (s *SQLite) ClaimNextPendingTranscription(ctx context.Context) (*Recording, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin Transcription claim: %w", err)
	}
	defer tx.Rollback()
	var result Recording
	var startedAt, duration, queuedAt int64
	err = tx.QueryRowContext(ctx, `SELECT id, title, started_at_unix_ns, duration_ns, audio_path, pending_promotion, interrupted, transcription_status, transcription_failure_category, transcription_error, transcription_queued_at_unix_ns, transcription_attempt FROM recordings WHERE transcription_status = ? AND pending_promotion = 0 AND interrupted = 0 ORDER BY transcription_queued_at_unix_ns, started_at_unix_ns, id LIMIT 1`, TranscriptionPending).Scan(&result.ID, &result.Title, &startedAt, &duration, &result.AudioPath, &result.PendingPromotion, &result.Interrupted, &result.TranscriptionStatus, &result.TranscriptionFailureCategory, &result.TranscriptionError, &queuedAt, &result.TranscriptionAttempt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select pending Transcription: %w", err)
	}
	changed, err := tx.ExecContext(ctx, `UPDATE recordings SET transcription_status = ?, transcription_failure_category = '', transcription_error = '', transcription_attempt = transcription_attempt + 1 WHERE id = ? AND transcription_status = ? AND transcription_attempt = ?`, TranscriptionRunning, result.ID, TranscriptionPending, result.TranscriptionAttempt)
	if err != nil {
		return nil, fmt.Errorf("claim pending Transcription: %w", err)
	}
	count, err := changed.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("check claimed Transcription: %w", err)
	}
	if count == 0 {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Transcription claim: %w", err)
	}
	result.StartedAt = time.Unix(0, startedAt).UTC()
	result.Duration = time.Duration(duration)
	result.TranscriptionStatus = TranscriptionRunning
	result.TranscriptionQueuedAt = time.Unix(0, queuedAt).UTC()
	result.TranscriptionAttempt++
	return &result, nil
}

// ResetRunningTranscriptions makes work interrupted by a prior process eligible again.
func (s *SQLite) ResetRunningTranscriptions(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE recordings SET transcription_status = ?, transcription_failure_category = '', transcription_error = '', transcription_queued_at_unix_ns = ? WHERE transcription_status = ?`, TranscriptionPending, time.Now().UnixNano(), TranscriptionRunning); err != nil {
		return fmt.Errorf("recover running Transcriptions: %w", err)
	}
	return nil
}

// CompleteTranscriptionAttempt atomically replaces segments only if the attempt still owns the Recording.
func (s *SQLite) CompleteTranscriptionAttempt(ctx context.Context, id string, attempt uint64, segments []Segment) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin Transcription completion: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE recordings SET transcription_status = ?, transcription_failure_category = '', transcription_error = '', transcription_queued_at_unix_ns = 0 WHERE id = ? AND transcription_status = ? AND transcription_attempt = ?`, TranscriptionSucceeded, id, TranscriptionRunning, attempt)
	if err != nil {
		return false, fmt.Errorf("complete Transcription for Recording %q: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check completed Transcription for Recording %q: %w", id, err)
	}
	if changed == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM transcription_segments WHERE recording_id = ?`, id); err != nil {
		return false, fmt.Errorf("clear Transcription for Recording %q: %w", id, err)
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO transcription_segments (recording_id, start_ns, end_ns, text) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return false, fmt.Errorf("prepare Transcription completion: %w", err)
	}
	defer statement.Close()
	for _, segment := range segments {
		if _, err := statement.ExecContext(ctx, id, segment.Start.Nanoseconds(), segment.End.Nanoseconds(), segment.Text); err != nil {
			return false, fmt.Errorf("save Transcription segment for Recording %q: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit Transcription completion: %w", err)
	}
	return true, nil
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
		INSERT INTO recordings (id, title, started_at_unix_ns, duration_ns, audio_path, pending_promotion, interrupted, transcription_status, transcription_failure_category, transcription_error, transcription_queued_at_unix_ns, transcription_attempt)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			started_at_unix_ns = excluded.started_at_unix_ns,
			duration_ns = excluded.duration_ns,
			audio_path = excluded.audio_path,
			pending_promotion = excluded.pending_promotion,
			interrupted = excluded.interrupted,
			transcription_status = excluded.transcription_status,
			transcription_failure_category = excluded.transcription_failure_category,
			transcription_error = excluded.transcription_error,
			transcription_queued_at_unix_ns = excluded.transcription_queued_at_unix_ns,
			transcription_attempt = excluded.transcription_attempt
	`, recording.ID, recording.Title, recording.StartedAt.UnixNano(), recording.Duration.Nanoseconds(), recording.AudioPath, recording.PendingPromotion, recording.Interrupted, transcriptionStatus(recording.TranscriptionStatus), recording.TranscriptionFailureCategory, recording.TranscriptionError, recording.TranscriptionQueuedAt.UnixNano(), recording.TranscriptionAttempt)
	if err != nil {
		return fmt.Errorf("save Recording %q: %w", recording.ID, err)
	}

	return nil
}

func transcriptionStatus(status TranscriptionStatus) TranscriptionStatus {
	if status == "" {
		return TranscriptionNotQueued
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
		SELECT id, title, started_at_unix_ns, duration_ns, audio_path, pending_promotion, interrupted, transcription_status, transcription_failure_category, transcription_error, transcription_queued_at_unix_ns, transcription_attempt, summary_title, summary_overview, summary_agreements, summary_suggestions, summary_action_items, summary_deadlines, summary_open_questions, summary_status, summary_failure_category, summary_error, summary_queued_at_unix_ns, summary_attempt
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
		var startedAt, duration, queuedAt, summaryQueuedAt int64
		var agreements, suggestions, actions, deadlines, questions string
		if err := rows.Scan(&recording.ID, &recording.Title, &startedAt, &duration, &recording.AudioPath, &recording.PendingPromotion, &recording.Interrupted, &recording.TranscriptionStatus, &recording.TranscriptionFailureCategory, &recording.TranscriptionError, &queuedAt, &recording.TranscriptionAttempt, &recording.Summary.Title, &recording.Summary.Overview, &agreements, &suggestions, &actions, &deadlines, &questions, &recording.SummaryStatus, &recording.SummaryFailureCategory, &recording.SummaryError, &summaryQueuedAt, &recording.SummaryAttempt); err != nil {
			return nil, fmt.Errorf("read Recording history: %w", err)
		}

		recording.StartedAt = time.Unix(0, startedAt).UTC()
		recording.Duration = time.Duration(duration)
		recording.TranscriptionQueuedAt = time.Unix(0, queuedAt).UTC()
		if err := decodeSummary(&recording, agreements, suggestions, actions, deadlines, questions, summaryQueuedAt); err != nil {
			return nil, err
		}
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
	var queuedAt int64
	var summaryQueuedAt int64
	var agreements, suggestions, actions, deadlines, questions string
	err := s.db.QueryRowContext(ctx, `SELECT id, title, started_at_unix_ns, duration_ns, audio_path, pending_promotion, interrupted, transcription_status, transcription_failure_category, transcription_error, transcription_queued_at_unix_ns, transcription_attempt, summary_title, summary_overview, summary_agreements, summary_suggestions, summary_action_items, summary_deadlines, summary_open_questions, summary_status, summary_failure_category, summary_error, summary_queued_at_unix_ns, summary_attempt FROM recordings WHERE id = ?`, id).Scan(&result.ID, &result.Title, &startedAt, &duration, &result.AudioPath, &result.PendingPromotion, &result.Interrupted, &result.TranscriptionStatus, &result.TranscriptionFailureCategory, &result.TranscriptionError, &queuedAt, &result.TranscriptionAttempt, &result.Summary.Title, &result.Summary.Overview, &agreements, &suggestions, &actions, &deadlines, &questions, &result.SummaryStatus, &result.SummaryFailureCategory, &result.SummaryError, &summaryQueuedAt, &result.SummaryAttempt)
	if err == sql.ErrNoRows {
		return Recording{}, fmt.Errorf("load Recording %q: not found", id)
	}
	if err != nil {
		return Recording{}, fmt.Errorf("load Recording %q: %w", id, err)
	}
	result.StartedAt = time.Unix(0, startedAt).UTC()
	result.Duration = time.Duration(duration)
	result.TranscriptionQueuedAt = time.Unix(0, queuedAt).UTC()
	if err := decodeSummary(&result, agreements, suggestions, actions, deadlines, questions, summaryQueuedAt); err != nil {
		return Recording{}, err
	}
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

func decodeSummary(r *Recording, agreements, suggestions, actions, deadlines, questions string, queued int64) error {
	for _, pair := range []struct {
		raw    string
		target *[]string
	}{{agreements, &r.Summary.Agreements}, {suggestions, &r.Summary.Suggestions}, {actions, &r.Summary.ActionItems}, {deadlines, &r.Summary.Deadlines}, {questions, &r.Summary.OpenQuestions}} {
		if err := json.Unmarshal([]byte(pair.raw), pair.target); err != nil {
			return fmt.Errorf("read Summary for Recording %q: %w", r.ID, err)
		}
	}
	r.SummaryQueuedAt = time.Unix(0, queued).UTC()
	return nil
}

// Close releases the local database connection.
func (s *SQLite) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close Recording history database: %w", err)
	}

	return nil
}
