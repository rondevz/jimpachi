package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"jimpachi/internal/audio"
	"jimpachi/internal/recording"
)

// Workflow coordinates Jimpachi behavior for the terminal UI.
type Workflow struct {
	history *recording.SQLite
	audio   audio.Adapter

	audioSourceMu            sync.Mutex
	latestAudioSourceVersion uint64
}

const recordingResponsibilityReminder = "Record system output only when everyone involved knows and agrees. You are responsible for complying with applicable laws and policies."

var errStaleAudioSourceSelection = errors.New("stale Audio source selection")

// Open creates a workflow backed by local Recording history.
func Open(ctx context.Context, dataDir string) (*Workflow, error) {
	return OpenWithAudio(ctx, dataDir, audio.New())
}

// OpenWithAudio creates a workflow using an operating-system audio adapter.
func OpenWithAudio(ctx context.Context, dataDir string, adapter audio.Adapter) (*Workflow, error) {
	history, err := recording.OpenSQLite(ctx, dataDir)
	if err != nil {
		return nil, fmt.Errorf("open application workflow: %w", err)
	}

	return &Workflow{history: history, audio: adapter}, nil
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

// Startup returns initial user-visible application state.
func (w *Workflow) Startup(ctx context.Context) (Startup, error) {
	startup := Startup{}
	seen, ok, err := w.history.Setting(ctx, "recording_responsibility_reminder_seen")
	if err != nil {
		return Startup{}, fmt.Errorf("load recording-responsibility reminder state: %w", err)
	}
	if !ok || seen != "true" {
		startup.Reminder = recordingResponsibilityReminder
		if err := w.history.SaveSetting(ctx, "recording_responsibility_reminder_seen", "true"); err != nil {
			return Startup{}, fmt.Errorf("save recording-responsibility reminder state: %w", err)
		}
	}

	return startup, nil
}

// Startup is the initial user-visible state of a Workflow.
type Startup struct {
	Reminder string
}

// AudioState contains source choices and any discovery guidance for the UI.
type AudioState struct {
	Sources            []audio.Source
	Selected           audio.Source
	DiscoveryError     string
	CanUseExplicitPath bool
}

// AudioSources discovers system-output sources and restores the selected one.
func (w *Workflow) AudioSources(ctx context.Context) (AudioState, error) {
	sources, discoveryErr := w.audio.Sources(ctx)
	state := AudioState{Sources: sources}
	if discoveryErr != nil {
		if len(sources) == 0 {
			state.DiscoveryError = fmt.Sprintf("Could not discover system-output sources: %v. Press a to enter a monitor source path.", discoveryErr)
			state.CanUseExplicitPath = true
		} else {
			state.DiscoveryError = discoveryErr.Error()
		}
	}

	selectedID, ok, err := w.history.Setting(ctx, "audio_source_id")
	if err != nil {
		return AudioState{}, fmt.Errorf("load selected Audio source: %w", err)
	}
	if ok {
		selectedName, _, err := w.history.Setting(ctx, "audio_source_name")
		if err != nil {
			return AudioState{}, fmt.Errorf("load selected Audio source name: %w", err)
		}
		explicit, _, err := w.history.Setting(ctx, "audio_source_explicit")
		if err != nil {
			return AudioState{}, fmt.Errorf("load selected Audio source type: %w", err)
		}
		state.Selected = audio.Source{ID: selectedID, Name: selectedName, Explicit: explicit == "true"}
		found := false
		for _, source := range state.Sources {
			if source.ID == selectedID {
				found = true
				break
			}
		}
		if !found {
			state.Sources = append([]audio.Source{state.Selected}, state.Sources...)
		}
		return state, nil
	}
	if len(sources) > 0 {
		state.Selected = sources[0]
	}

	return state, nil
}

// SelectAudioSource persists the newest Audio source selected by the user.
func (w *Workflow) SelectAudioSource(ctx context.Context, source audio.Source, version uint64) error {
	if source.ID == "" {
		return fmt.Errorf("select Audio source: source path is required")
	}
	if source.Name == "" {
		source.Name = source.ID
	}
	w.audioSourceMu.Lock()
	defer w.audioSourceMu.Unlock()
	if version <= w.latestAudioSourceVersion {
		return errStaleAudioSourceSelection
	}
	w.latestAudioSourceVersion = version
	if err := w.history.SaveSettings(ctx, map[string]string{
		"audio_source_id":       source.ID,
		"audio_source_name":     source.Name,
		"audio_source_explicit": fmt.Sprintf("%t", source.Explicit),
	}); err != nil {
		return fmt.Errorf("save selected Audio source: %w", err)
	}

	return nil
}

// AudioActivity returns the current normalized activity for an Audio source.
func (w *Workflow) AudioActivity(ctx context.Context, source audio.Source) (float64, error) {
	activity, err := w.audio.Activity(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("measure Audio source activity: %w", err)
	}
	if activity < 0 {
		return 0, nil
	}
	if activity > 1 {
		return 1, nil
	}

	return activity, nil
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
