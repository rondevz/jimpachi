package recording

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteMigratesLegacyFailedTranscriptionToExecutionCategory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(dir, "jimpachi.db"))
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE recordings (id TEXT PRIMARY KEY, title TEXT NOT NULL, started_at_unix_ns INTEGER NOT NULL, duration_ns INTEGER NOT NULL, audio_path TEXT NOT NULL, pending_promotion INTEGER NOT NULL DEFAULT 0, interrupted INTEGER NOT NULL DEFAULT 0, transcription_status TEXT NOT NULL DEFAULT 'pending', transcription_error TEXT NOT NULL DEFAULT ''); INSERT INTO recordings (id, title, started_at_unix_ns, duration_ns, audio_path, transcription_status, transcription_error) VALUES ('recording-1', 'Instructions', 0, 0, 'missing.opus', 'failed', 'Transcription could not be completed.');`)
	if err != nil {
		t.Fatalf("create legacy database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	store, err := OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	detail, err := store.Recording(ctx, "recording-1")
	if err != nil {
		t.Fatalf("Recording() error = %v", err)
	}
	if detail.TranscriptionFailureCategory != ProcessingFailureExecution {
		t.Errorf("legacy failure category = %q, want execution", detail.TranscriptionFailureCategory)
	}
}

func TestSQLiteDoesNotSilentlyCancelAClaimedTranscription(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	path := filepath.Join(t.TempDir(), "recording.opus")
	if err := os.WriteFile(path, []byte("opus"), 0o600); err != nil {
		t.Fatalf("create Recording audio: %v", err)
	}
	if err := store.Save(ctx, Recording{ID: "recording-1", Title: "Instructions", StartedAt: time.Now(), AudioPath: path}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.QueueTranscription(ctx, "recording-1"); err != nil {
		t.Fatalf("QueueTranscription() error = %v", err)
	}
	if _, err := store.ClaimNextPendingTranscription(ctx); err != nil {
		t.Fatalf("ClaimNextPendingTranscription() error = %v", err)
	}
	cancelled, err := store.CancelQueuedTranscription(ctx, "recording-1")
	if err != nil {
		t.Fatalf("CancelQueuedTranscription() error = %v", err)
	}
	if cancelled {
		t.Error("CancelQueuedTranscription() cancelled an already claimed attempt")
	}
}

func TestSQLitePersistsRecordingHistoryAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	recordingsDir := filepath.Join(dir, "recordings")
	if err := os.MkdirAll(recordingsDir, 0o700); err != nil {
		t.Fatalf("create recordings directory: %v", err)
	}
	earlierPath := filepath.Join(recordingsDir, "earlier.opus")
	laterPath := filepath.Join(recordingsDir, "later.opus")
	for _, path := range []string{earlierPath, laterPath} {
		if err := os.WriteFile(path, []byte("opus"), 0o600); err != nil {
			t.Fatalf("create Recording audio: %v", err)
		}
	}

	store, err := OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}

	earlier := Recording{
		ID:        "recording-earlier",
		Title:     "Earlier instructions",
		StartedAt: time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC),
		Duration:  5 * time.Minute,
		AudioPath: earlierPath,
	}
	later := Recording{
		ID:        "recording-later",
		Title:     "Later instructions",
		StartedAt: time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC),
		Duration:  8 * time.Minute,
		AudioPath: laterPath,
	}

	if err := store.Save(ctx, earlier); err != nil {
		t.Fatalf("Save(earlier) error = %v", err)
	}
	if err := store.Save(ctx, later); err != nil {
		t.Fatalf("Save(later) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatalf("OpenSQLite() for history error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	history, err := reopened.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}

	if got, want := len(history), 2; got != want {
		t.Fatalf("History() returned %d recordings, want %d", got, want)
	}
	if got := history[0]; got.ID != later.ID || got.Title != later.Title || got.StartedAt != later.StartedAt || got.Duration != later.Duration || got.AudioPath != later.AudioPath {
		t.Errorf("History()[0] = %#v, want %#v", got, later)
	}
	if got := history[1]; got.ID != earlier.ID || got.Title != earlier.Title || got.StartedAt != earlier.StartedAt || got.Duration != earlier.Duration || got.AudioPath != earlier.AudioPath {
		t.Errorf("History()[1] = %#v, want %#v", got, earlier)
	}

	if _, err := os.Stat(filepath.Join(dir, "jimpachi.db")); err != nil {
		t.Errorf("database file was not created: %v", err)
	}
}

func TestSQLiteSaveSettingsRollsBackAllValuesWhenOneWriteFails(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.db.ExecContext(ctx, `
		CREATE TRIGGER reject_audio_source_name
		BEFORE INSERT ON settings WHEN NEW.key = 'audio_source_name'
		BEGIN SELECT RAISE(ABORT, 'reject source name'); END;
	`); err != nil {
		t.Fatalf("create settings trigger error = %v", err)
	}

	err = store.SaveSettings(ctx, map[string]string{
		"audio_source_id":       "speakers.monitor",
		"audio_source_name":     "Speakers",
		"audio_source_explicit": "false",
	})
	if err == nil {
		t.Fatal("SaveSettings() error = nil, want transaction failure")
	}
	for _, key := range []string{"audio_source_id", "audio_source_name", "audio_source_explicit"} {
		if _, found, err := store.Setting(ctx, key); err != nil {
			t.Fatalf("Setting(%q) error = %v", key, err)
		} else if found {
			t.Errorf("Setting(%q) remained after failed atomic save", key)
		}
	}
}

func TestSQLiteReconcilesRecordingWhoseFinalAudioWasNeverPromoted(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Save(ctx, Recording{ID: "interrupted", Title: "Interrupted", StartedAt: time.Now(), AudioPath: t.TempDir() + "/interrupted.opus", PendingPromotion: true}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := store.ReconcileAudio(ctx); err != nil {
		t.Fatalf("ReconcileAudio() error = %v", err)
	}
	history, err := store.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 0 {
		t.Errorf("History() = %#v, want interrupted Recording removed", history)
	}
}

func TestSQLitePromotesStagedAudioForInterruptedRecording(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	finalPath := filepath.Join(dir, "recordings", "interrupted.opus")
	stagedPath := filepath.Join(dir, "recordings", "interrupted.partial.opus")
	if err := os.MkdirAll(filepath.Dir(stagedPath), 0o700); err != nil {
		t.Fatalf("create recordings directory: %v", err)
	}
	if err := os.WriteFile(stagedPath, []byte("opus"), 0o600); err != nil {
		t.Fatalf("create staged audio: %v", err)
	}
	if err := store.Save(ctx, Recording{ID: "interrupted", Title: "Interrupted", StartedAt: time.Now(), AudioPath: finalPath, PendingPromotion: true}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := store.ReconcileAudio(ctx); err != nil {
		t.Fatalf("ReconcileAudio() error = %v", err)
	}
	history, err := store.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 1 || history[0].AudioPath != finalPath {
		t.Errorf("History() = %#v, want recovered Recording", history)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("final audio was not promoted: %v", err)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Errorf("staged audio remains after recovery: stat error = %v", err)
	}
}

func TestSQLiteKeepsCompletedRecordingWhenAudioIsMissing(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	path := t.TempDir() + "/moved.opus"
	if err := store.Save(ctx, Recording{ID: "moved", Title: "Moved", StartedAt: time.Now(), AudioPath: path}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.ReconcileAudio(ctx); err != nil {
		t.Fatalf("ReconcileAudio() error = %v", err)
	}
	history, err := store.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 1 || !history[0].AudioMissing {
		t.Errorf("History() = %#v, want completed Recording with missing-audio state", history)
	}
}

func TestSQLitePersistsTimestampedTranscriptionSegments(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	path := filepath.Join(t.TempDir(), "recording.opus")
	if err := os.WriteFile(path, []byte("opus"), 0o600); err != nil {
		t.Fatalf("create Recording audio: %v", err)
	}
	if err := store.Save(ctx, Recording{ID: "recording-1", Title: "Instructions", StartedAt: time.Now(), AudioPath: path}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	segments := []Segment{{Start: time.Second, End: 3 * time.Second, Text: "Deploy the service."}, {Start: 4 * time.Second, End: 6 * time.Second, Text: "Verify the dashboard."}}
	if err := store.SaveTranscription(ctx, "recording-1", segments); err != nil {
		t.Fatalf("SaveTranscription() error = %v", err)
	}
	detail, err := store.Recording(ctx, "recording-1")
	if err != nil {
		t.Fatalf("Recording() error = %v", err)
	}
	if got, want := len(detail.Transcription), 2; got != want {
		t.Fatalf("Transcription = %#v, want two segments", detail.Transcription)
	}
	for index, want := range segments {
		if got := detail.Transcription[index]; got != want {
			t.Errorf("Transcription[%d] = %#v, want %#v", index, got, want)
		}
	}
}

func TestSQLiteTracksTranscriptionStatusAndEnforcesSegmentParent(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveTranscription(ctx, "missing", nil); err == nil {
		t.Fatal("SaveTranscription() error = nil, want missing Recording rejection")
	}
	path := filepath.Join(t.TempDir(), "recording.opus")
	if err := os.WriteFile(path, []byte("opus"), 0o600); err != nil {
		t.Fatalf("create Recording audio: %v", err)
	}
	if err := store.Save(ctx, Recording{ID: "recording-1", Title: "Instructions", StartedAt: time.Now(), AudioPath: path}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.SaveTranscriptionStatus(ctx, "recording-1", TranscriptionFailed, "Transcription could not be completed."); err != nil {
		t.Fatalf("SaveTranscriptionStatus() error = %v", err)
	}
	if err := store.SaveTranscription(ctx, "recording-1", []Segment{{Text: "Deploy."}}); err != nil {
		t.Fatalf("SaveTranscription() error = %v", err)
	}
	detail, err := store.Recording(ctx, "recording-1")
	if err != nil {
		t.Fatalf("Recording() error = %v", err)
	}
	if detail.TranscriptionStatus != TranscriptionFailed || detail.TranscriptionError != "Transcription could not be completed." {
		t.Errorf("Recording() Transcription state = %q, %q", detail.TranscriptionStatus, detail.TranscriptionError)
	}
	if err := store.Delete(ctx, "recording-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	var remaining int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transcription_segments`).Scan(&remaining); err != nil {
		t.Fatalf("count segments: %v", err)
	}
	if remaining != 0 {
		t.Errorf("segments after Recording deletion = %d, want 0", remaining)
	}
}
