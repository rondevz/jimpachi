package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"jimpachi/internal/recording"
)

// Workflow coordinates Jimpachi behavior for the terminal UI.
type Workflow struct {
	history *recording.SQLite
}

// Open creates a workflow backed by local Recording history.
func Open(ctx context.Context, dataDir string) (*Workflow, error) {
	history, err := recording.OpenSQLite(ctx, dataDir)
	if err != nil {
		return nil, fmt.Errorf("open application workflow: %w", err)
	}

	return &Workflow{history: history}, nil
}

// DataDir returns the XDG data directory for Jimpachi.
func DataDir() (string, error) {
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		if !filepath.IsAbs(dataHome) {
			return "", fmt.Errorf("XDG_DATA_HOME must be an absolute path")
		}

		return filepath.Join(dataHome, "jimpachi"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home directory: %w", err)
	}

	return filepath.Join(home, ".local", "share", "jimpachi"), nil
}

// History returns Recordings ordered from newest to oldest.
func (w *Workflow) History(ctx context.Context) ([]recording.Recording, error) {
	history, err := w.history.History(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow Recording history: %w", err)
	}

	return history, nil
}

// Close releases the workflow's local resources.
func (w *Workflow) Close() error {
	if err := w.history.Close(); err != nil {
		return fmt.Errorf("close application workflow: %w", err)
	}

	return nil
}
