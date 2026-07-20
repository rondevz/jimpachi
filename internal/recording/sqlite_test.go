package recording

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLitePersistsRecordingHistoryAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}

	earlier := Recording{
		ID:        "recording-earlier",
		Title:     "Earlier instructions",
		StartedAt: time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC),
		Duration:  5 * time.Minute,
		AudioPath: "/recordings/earlier.opus",
	}
	later := Recording{
		ID:        "recording-later",
		Title:     "Later instructions",
		StartedAt: time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC),
		Duration:  8 * time.Minute,
		AudioPath: "/recordings/later.opus",
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
	if got, want := history[0], later; got != want {
		t.Errorf("History()[0] = %#v, want %#v", got, want)
	}
	if got, want := history[1], earlier; got != want {
		t.Errorf("History()[1] = %#v, want %#v", got, want)
	}

	if _, err := os.Stat(filepath.Join(dir, "jimpachi.db")); err != nil {
		t.Errorf("database file was not created: %v", err)
	}
}
