package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jimpachi/internal/recording"
)

func TestOpenCreatesWorkflowWithEmptyRecordingHistory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	workflow, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })

	history, err := workflow.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 0 {
		t.Errorf("History() = %#v, want no Recordings", history)
	}

	if _, err := os.Stat(filepath.Join(dir, "jimpachi.db")); err != nil {
		t.Errorf("Recording history database was not created: %v", err)
	}
}

func TestDataDirUsesXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/jimpachi-data")

	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	if want := "/tmp/jimpachi-data/jimpachi"; got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}

func TestDataDirRejectsRelativeXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "jimpachi-data")

	if _, err := DataDir(); err == nil {
		t.Fatal("DataDir() error = nil, want relative XDG data home error")
	}
}

func TestWorkflowShowsRecordingHistoryAfterReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := recording.OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	if err := store.Save(ctx, recording.Recording{
		ID:        "recording-1",
		Title:     "Deployment instructions",
		StartedAt: time.Date(2026, time.July, 20, 10, 30, 0, 0, time.UTC),
		Duration:  18 * time.Minute,
		AudioPath: "/recordings/deployment.opus",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	workflow, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("Open() after reopen error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })

	history, err := workflow.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if got, want := len(history), 1; got != want {
		t.Fatalf("History() returned %d Recordings, want %d", got, want)
	}
	if got, want := history[0].Title, "Deployment instructions"; got != want {
		t.Errorf("History()[0].Title = %q, want %q", got, want)
	}
}
