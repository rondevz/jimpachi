package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jimpachi/internal/audio"
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

func TestWorkflowStartupShowsRecordingResponsibilityReminderOnlyOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	workflow, err := OpenWithAudio(ctx, dir, fakeAudio{})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	first, err := workflow.Startup(ctx)
	if err != nil {
		t.Fatalf("Startup() first call error = %v", err)
	}
	if first.Reminder == "" {
		t.Error("Startup() first call did not show the recording-responsibility reminder")
	}
	second, err := workflow.Startup(ctx)
	if err != nil {
		t.Fatalf("Startup() second call error = %v", err)
	}
	if second.Reminder != "" {
		t.Errorf("Startup() second call reminder = %q, want no reminder", second.Reminder)
	}
	if err := workflow.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := OpenWithAudio(ctx, dir, fakeAudio{})
	if err != nil {
		t.Fatalf("OpenWithAudio() after reopen error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	startup, err := reopened.Startup(ctx)
	if err != nil {
		t.Fatalf("Startup() after reopen error = %v", err)
	}
	if startup.Reminder != "" {
		t.Errorf("Startup() after reopen reminder = %q, want no reminder", startup.Reminder)
	}
}

func TestWorkflowSelectsAndRestoresAudioSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	speakers := audio.Source{ID: "alsa_output.usb-speakers.monitor", Name: "USB speakers"}
	headphones := audio.Source{ID: "alsa_output.headphones.monitor", Name: "Headphones"}

	workflow, err := OpenWithAudio(ctx, dir, fakeAudio{sources: []audio.Source{speakers, headphones}})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	state, err := workflow.AudioSources(ctx)
	if err != nil {
		t.Fatalf("AudioSources() error = %v", err)
	}
	if state.Selected != speakers {
		t.Errorf("AudioSources().Selected = %#v, want %#v", state.Selected, speakers)
	}
	if err := workflow.SelectAudioSource(ctx, headphones, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if err := workflow.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := OpenWithAudio(ctx, dir, fakeAudio{sources: []audio.Source{speakers, headphones}})
	if err != nil {
		t.Fatalf("OpenWithAudio() after reopen error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	state, err = reopened.AudioSources(ctx)
	if err != nil {
		t.Fatalf("AudioSources() after reopen error = %v", err)
	}
	if state.Selected != headphones {
		t.Errorf("AudioSources() after reopen selected = %#v, want %#v", state.Selected, headphones)
	}
}

func TestWorkflowPersistsLatestAudioSourceWhenEarlierSaveCompletesLast(t *testing.T) {
	ctx := context.Background()
	first := audio.Source{ID: "first.monitor", Name: "First"}
	second := audio.Source{ID: "second.monitor", Name: "Second"}
	workflow, err := OpenWithAudio(ctx, t.TempDir(), fakeAudio{})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })

	releaseFirstSave := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		<-releaseFirstSave
		firstDone <- workflow.SelectAudioSource(ctx, first, 1)
	}()
	if err := workflow.SelectAudioSource(ctx, second, 2); err != nil {
		t.Fatalf("SelectAudioSource(second) error = %v", err)
	}
	close(releaseFirstSave)
	if err := <-firstDone; !errors.Is(err, errStaleAudioSourceSelection) {
		t.Fatalf("SelectAudioSource(first) error = %v, want stale selection error", err)
	}

	state, err := workflow.AudioSources(ctx)
	if err != nil {
		t.Fatalf("AudioSources() error = %v", err)
	}
	if state.Selected != second {
		t.Errorf("AudioSources().Selected = %#v, want latest confirmed source %#v", state.Selected, second)
	}
}

func TestWorkflowDoesNotPersistDiscoveredAudioSourceUntilUserConfirmsIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first := audio.Source{ID: "first.monitor", Name: "First"}
	second := audio.Source{ID: "second.monitor", Name: "Second"}

	workflow, err := OpenWithAudio(ctx, dir, fakeAudio{sources: []audio.Source{first, second}})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	state, err := workflow.AudioSources(ctx)
	if err != nil {
		t.Fatalf("AudioSources() error = %v", err)
	}
	if state.Selected != first {
		t.Errorf("AudioSources().Selected = %#v, want %#v", state.Selected, first)
	}
	if err := workflow.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := OpenWithAudio(ctx, dir, fakeAudio{sources: []audio.Source{second, first}})
	if err != nil {
		t.Fatalf("OpenWithAudio() after reopen error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	state, err = reopened.AudioSources(ctx)
	if err != nil {
		t.Fatalf("AudioSources() after reopen error = %v", err)
	}
	if state.Selected != second {
		t.Errorf("AudioSources() after reopen selected = %#v, want %#v", state.Selected, second)
	}
}

func TestWorkflowOffersExplicitPathWhenAudioSourceDiscoveryUnavailable(t *testing.T) {
	ctx := context.Background()
	workflow, err := OpenWithAudio(ctx, t.TempDir(), fakeAudio{err: os.ErrNotExist})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })

	state, err := workflow.AudioSources(ctx)
	if err != nil {
		t.Fatalf("AudioSources() error = %v", err)
	}
	if state.DiscoveryError == "" {
		t.Error("AudioSources() did not report unavailable source discovery")
	}
	if !state.CanUseExplicitPath {
		t.Error("AudioSources() did not offer an explicit source path")
	}

	explicit := audio.Source{ID: "alsa_output.manual.monitor", Name: "alsa_output.manual.monitor", Explicit: true}
	if err := workflow.SelectAudioSource(ctx, explicit, 1); err != nil {
		t.Fatalf("SelectAudioSource() explicit path error = %v", err)
	}
}

func TestWorkflowRestoresConfirmedExplicitAudioSourceWhenDiscoveryHasNoSources(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	explicit := audio.Source{ID: "alsa_output.manual.monitor", Name: "Manual monitor", Explicit: true}

	workflow, err := OpenWithAudio(ctx, dir, fakeAudio{err: os.ErrNotExist})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	if err := workflow.SelectAudioSource(ctx, explicit, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if err := workflow.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := OpenWithAudio(ctx, dir, fakeAudio{err: os.ErrNotExist})
	if err != nil {
		t.Fatalf("OpenWithAudio() after reopen error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	state, err := reopened.AudioSources(ctx)
	if err != nil {
		t.Fatalf("AudioSources() error = %v", err)
	}
	if len(state.Sources) != 1 || state.Sources[0] != explicit {
		t.Errorf("AudioSources().Sources = %#v, want confirmed explicit source", state.Sources)
	}
}

func TestWorkflowReturnsActivityForAudioSource(t *testing.T) {
	ctx := context.Background()
	source := audio.Source{ID: "alsa_output.usb-speakers.monitor", Name: "USB speakers"}
	workflow, err := OpenWithAudio(ctx, t.TempDir(), fakeAudio{activity: 0.75})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })

	activity, err := workflow.AudioActivity(ctx, source)
	if err != nil {
		t.Fatalf("AudioActivity() error = %v", err)
	}
	if activity != 0.75 {
		t.Errorf("AudioActivity() = %v, want 0.75", activity)
	}
}

type fakeAudio struct {
	sources  []audio.Source
	err      error
	activity float64
}

func (f fakeAudio) Sources(context.Context) ([]audio.Source, error) {
	return f.sources, f.err
}

func (f fakeAudio) Activity(context.Context, audio.Source) (float64, error) {
	return f.activity, nil
}
