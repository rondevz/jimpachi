package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	audioPath := filepath.Join(dir, "recordings", "deployment.opus")
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o700); err != nil {
		t.Fatalf("create recording directory: %v", err)
	}
	if err := os.WriteFile(audioPath, []byte("opus"), 0o600); err != nil {
		t.Fatalf("create Recording audio: %v", err)
	}

	store, err := recording.OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	if err := store.Save(ctx, recording.Recording{
		ID:        "recording-1",
		Title:     "Deployment instructions",
		StartedAt: time.Date(2026, time.July, 20, 10, 30, 0, 0, time.UTC),
		Duration:  18 * time.Minute,
		AudioPath: audioPath,
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

func TestWorkflowCapturesSelectedAudioSourceAndSavesRecording(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	capture := &fakeCapture{}
	workflow, err := OpenWithAudio(ctx, dir, fakeAudio{capture: capture})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	if err := workflow.SelectAudioSource(ctx, source, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}

	active, err := workflow.StartRecording(ctx)
	if err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}
	if active.Source != source {
		t.Errorf("StartRecording().Source = %#v, want %#v", active.Source, source)
	}
	if capture.source != source {
		t.Errorf("capture source = %#v, want %#v", capture.source, source)
	}

	completed, err := workflow.StopRecording(ctx)
	if err != nil {
		t.Fatalf("StopRecording() error = %v", err)
	}
	if completed.ID == "" {
		t.Error("StopRecording().ID is empty")
	}
	if completed.Title == "" {
		t.Error("StopRecording().Title is empty")
	}
	if completed.StartedAt.IsZero() {
		t.Error("StopRecording().StartedAt is zero")
	}
	if completed.AudioPath == "" {
		t.Error("StopRecording().AudioPath is empty")
	}
	if _, err := os.Stat(completed.AudioPath); err != nil {
		t.Errorf("promoted audio %q was not saved: %v", completed.AudioPath, err)
	}
	temporary := strings.TrimSuffix(completed.AudioPath, ".opus") + ".partial.opus"
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Errorf("temporary audio remains after successful Recording: stat error = %v", err)
	}
	if !capture.stopped {
		t.Error("StopRecording() did not stop audio capture")
	}

	history, err := workflow.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 1 || history[0] != completed {
		t.Errorf("History() = %#v, want saved completed Recording %#v", history, completed)
	}
	if err := workflow.RenameRecording(ctx, completed.ID, "Deployment instructions"); err != nil {
		t.Fatalf("RenameRecording() error = %v", err)
	}
	history, err = workflow.History(ctx)
	if err != nil {
		t.Fatalf("History() after rename error = %v", err)
	}
	if got, want := history[0].Title, "Deployment instructions"; got != want {
		t.Errorf("renamed Recording title = %q, want %q", got, want)
	}
}

func TestWorkflowKeepsStagedAudioAfterPromotionFailureAndRetriesStop(t *testing.T) {
	ctx := context.Background()
	source := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	failingCapture := &fakeCapture{}
	adapter := fakeAudio{capture: failingCapture}
	workflow, err := OpenWithAudio(ctx, t.TempDir(), adapter)
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	if err := workflow.SelectAudioSource(ctx, source, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if _, err := workflow.StartRecording(ctx); err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}
	finalPath := strings.TrimSuffix(failingCapture.path, ".partial.opus") + ".opus"
	if err := os.Mkdir(finalPath, 0o700); err != nil {
		t.Fatalf("create conflicting final audio path: %v", err)
	}
	if _, err := workflow.StopRecording(ctx); err == nil {
		t.Fatal("StopRecording() error = nil, want promotion failure")
	}
	if _, err := os.Stat(failingCapture.path); err != nil {
		t.Errorf("staged audio %q was deleted after promotion failure: %v", failingCapture.path, err)
	}
	if _, err := workflow.StartRecording(ctx); err == nil {
		t.Fatal("StartRecording() error = nil, want retryable stopped Recording to remain active")
	}
	if err := os.Remove(finalPath); err != nil {
		t.Fatalf("remove conflicting final audio path: %v", err)
	}
	completed, err := workflow.StopRecording(ctx)
	if err != nil {
		t.Fatalf("StopRecording() retry error = %v", err)
	}
	if _, err := os.Stat(completed.AudioPath); err != nil {
		t.Errorf("retry did not promote final audio: %v", err)
	}
	history, err := workflow.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 1 || history[0].ID != completed.ID {
		t.Errorf("History() = %#v, want retried Recording %#v", history, completed)
	}
}

func TestWorkflowClearsUnexpectedCaptureFailureAndAllowsNextRecording(t *testing.T) {
	ctx := context.Background()
	source := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	capture := &fakeCapture{waitErr: errors.New("FFmpeg exited")}
	workflow, err := OpenWithAudio(ctx, t.TempDir(), fakeAudio{capture: capture})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	if err := workflow.SelectAudioSource(ctx, source, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if _, err := workflow.StartRecording(ctx); err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}

	deadline := time.After(time.Second)
	for workflow.CaptureState().Failure == "" {
		select {
		case <-deadline:
			t.Fatal("unexpected capture failure did not reach workflow state")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, err := os.Stat(capture.path); err != nil {
		t.Errorf("temporary audio %q was not preserved after unexpected failure: %v", capture.path, err)
	}
	capture.waitErr = nil
	if _, err := workflow.StartRecording(ctx); err != nil {
		t.Fatalf("StartRecording() after unexpected failure error = %v", err)
	}
}

func TestWorkflowCloseTreatsCaptureCancellationAsIntentional(t *testing.T) {
	ctx := context.Background()
	capture := &fakeCapture{waitUntilStopped: true, waitErr: context.Canceled, done: make(chan struct{})}
	workflow, err := OpenWithAudio(ctx, t.TempDir(), fakeAudio{capture: capture})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	if err := workflow.SelectAudioSource(ctx, audio.Source{ID: "speakers.monitor", Name: "Speakers"}, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if _, err := workflow.StartRecording(ctx); err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}
	if err := workflow.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if state := workflow.CaptureState(); state.Failure != "" {
		t.Errorf("CaptureState().Failure = %q, want no shutdown failure", state.Failure)
	}
}

func TestWorkflowRecordingDurationExcludesCaptureDrainTime(t *testing.T) {
	ctx := context.Background()
	capture := &fakeCapture{stopDelay: 50 * time.Millisecond}
	workflow, err := OpenWithAudio(ctx, t.TempDir(), fakeAudio{capture: capture})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	if err := workflow.SelectAudioSource(ctx, audio.Source{ID: "speakers.monitor", Name: "Speakers"}, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if _, err := workflow.StartRecording(ctx); err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}
	completed, err := workflow.StopRecording(ctx)
	if err != nil {
		t.Fatalf("StopRecording() error = %v", err)
	}
	if completed.Duration >= capture.stopDelay {
		t.Errorf("Recording duration = %v, includes %v of capture drain time", completed.Duration, capture.stopDelay)
	}
}

func TestWorkflowUsesAndPersistsDefaultRecordingLimit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	workflow, err := OpenWithAudio(ctx, dir, fakeAudio{})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	limit, err := workflow.RecordingLimit(ctx)
	if err != nil {
		t.Fatalf("RecordingLimit() error = %v", err)
	}
	if limit != time.Hour {
		t.Errorf("RecordingLimit() = %v, want 1h", limit)
	}
	if err := workflow.SetRecordingLimit(ctx, 25*time.Minute); err != nil {
		t.Fatalf("SetRecordingLimit() error = %v", err)
	}
	if err := workflow.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := OpenWithAudio(ctx, dir, fakeAudio{})
	if err != nil {
		t.Fatalf("OpenWithAudio() after reopen error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got, err := reopened.RecordingLimit(ctx); err != nil || got != 25*time.Minute {
		t.Errorf("RecordingLimit() after reopen = %v, %v; want 25m, nil", got, err)
	}
}

func TestWorkflowWarnsAndStopsRecordingAtConfiguredLimit(t *testing.T) {
	ctx := context.Background()
	clock := newFakeScheduler(time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC))
	capture := &fakeCapture{}
	workflow, err := OpenWithAudioAndScheduler(ctx, t.TempDir(), fakeAudio{capture: capture}, clock)
	if err != nil {
		t.Fatalf("OpenWithAudioAndScheduler() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	if err := workflow.SelectAudioSource(ctx, audio.Source{ID: "speakers.monitor", Name: "Speakers"}, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if err := workflow.SetRecordingLimit(ctx, 10*time.Minute); err != nil {
		t.Fatalf("SetRecordingLimit() error = %v", err)
	}
	if _, err := workflow.StartRecording(ctx); err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}

	clock.Advance(5 * time.Minute)
	if state := workflow.CaptureState(); state.Warning == "" || state.Active == nil {
		t.Errorf("CaptureState() at warning = %#v, want active Recording with warning", state)
	}
	clock.Advance(5 * time.Minute)
	state := workflow.CaptureState()
	if !capture.stopped {
		t.Error("configured limit did not stop capture")
	}
	if state.Active != nil || state.Completed == nil {
		t.Errorf("CaptureState() at limit = %#v, want completed Recording", state)
	}
	if state.Completed != nil && state.Completed.Duration != 10*time.Minute {
		t.Errorf("automatic Recording duration = %v, want 10m", state.Completed.Duration)
	}
}

func TestWorkflowPublishesAutomaticStopFinalizationFailureAndPreservesStagedAudio(t *testing.T) {
	ctx := context.Background()
	clock := newFakeScheduler(time.Now())
	capture := &fakeCapture{}
	workflow, err := OpenWithAudioAndScheduler(ctx, t.TempDir(), fakeAudio{capture: capture}, clock)
	if err != nil {
		t.Fatalf("OpenWithAudioAndScheduler() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	if err := workflow.SelectAudioSource(ctx, audio.Source{ID: "speakers.monitor", Name: "Speakers"}, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if err := workflow.SetRecordingLimit(ctx, time.Minute); err != nil {
		t.Fatalf("SetRecordingLimit() error = %v", err)
	}
	if _, err := workflow.StartRecording(ctx); err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}
	finalPath := strings.TrimSuffix(capture.path, ".partial.opus") + ".opus"
	if err := os.Mkdir(finalPath, 0o700); err != nil {
		t.Fatalf("create conflicting final path: %v", err)
	}
	clock.Advance(time.Minute)
	state := workflow.CaptureState()
	if state.Active != nil || !strings.Contains(state.Failure, "promote completed Recording audio") {
		t.Errorf("CaptureState() = %#v, want terminal promotion failure", state)
	}
	if _, err := os.Stat(capture.path); err != nil {
		t.Errorf("staged audio was not retained after automatic finalization failure: %v", err)
	}
}

func TestWorkflowDisabledRecordingLimitDoesNotScheduleStop(t *testing.T) {
	ctx := context.Background()
	clock := newFakeScheduler(time.Now())
	capture := &fakeCapture{}
	workflow, err := OpenWithAudioAndScheduler(ctx, t.TempDir(), fakeAudio{capture: capture}, clock)
	if err != nil {
		t.Fatalf("OpenWithAudioAndScheduler() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	if err := workflow.SelectAudioSource(ctx, audio.Source{ID: "speakers.monitor", Name: "Speakers"}, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if err := workflow.SetRecordingLimit(ctx, 0); err != nil {
		t.Fatalf("SetRecordingLimit() error = %v", err)
	}
	if _, err := workflow.StartRecording(ctx); err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}
	clock.Advance(2 * time.Hour)
	if state := workflow.CaptureState(); state.Active == nil || capture.stopped {
		t.Errorf("CaptureState() with disabled limit = %#v; capture stopped = %t", state, capture.stopped)
	}
}

func TestWorkflowRecoversPlayableInterruptedCaptureAfterRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := recording.OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	finalPath := filepath.Join(dir, "recordings", "interrupted.opus")
	partialPath := strings.TrimSuffix(finalPath, ".opus") + ".partial.opus"
	if err := os.MkdirAll(filepath.Dir(partialPath), 0o700); err != nil {
		t.Fatalf("create recordings directory: %v", err)
	}
	if err := os.WriteFile(partialPath, []byte("opus"), 0o600); err != nil {
		t.Fatalf("create partial audio: %v", err)
	}
	if err := store.Save(ctx, recording.Recording{ID: "interrupted", Title: "Interrupted", StartedAt: time.Now(), AudioPath: finalPath, PendingPromotion: true, Interrupted: true}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	workflow, err := OpenWithAudio(ctx, dir, fakeAudio{playable: true})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	history, err := workflow.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 1 || !history[0].Interrupted || history[0].AudioMissing {
		t.Errorf("History() = %#v, want playable interrupted Recording", history)
	}
}

func TestWorkflowPersistsPendingRecordingBeforeCaptureStartsAndRemovesItWhenStartFails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	observedPending := false
	partialPath := ""
	adapter := fakeAudio{startErr: errors.New("FFmpeg unavailable"), onStart: func(path string) {
		partialPath = path
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Errorf("create staged audio directory: %v", err)
		} else if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			t.Errorf("create staged audio: %v", err)
		}
		store, err := recording.OpenSQLite(ctx, dir)
		if err != nil {
			t.Errorf("OpenSQLite() while starting capture error = %v", err)
			return
		}
		defer store.Close()
		history, err := store.History(ctx)
		if err != nil {
			t.Errorf("History() while starting capture error = %v", err)
			return
		}
		observedPending = len(history) == 1 && history[0].PendingPromotion && history[0].Interrupted && history[0].AudioPath == strings.TrimSuffix(path, ".partial.opus")+".opus"
	}}
	workflow, err := OpenWithAudio(ctx, dir, adapter)
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	if err := workflow.SelectAudioSource(ctx, audio.Source{ID: "speakers.monitor", Name: "Speakers"}, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if _, err := workflow.StartRecording(ctx); err == nil {
		t.Fatal("StartRecording() error = nil, want capture start failure")
	}
	if !observedPending {
		t.Error("capture started before its pending Recording was persisted")
	}
	history, err := workflow.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 0 {
		t.Errorf("History() = %#v, want failed start removed", history)
	}
	if _, err := os.Stat(partialPath); !os.IsNotExist(err) {
		t.Errorf("staged audio remained after failed start: %v", err)
	}
}

func TestWorkflowReportsTimedOutAutomaticStopWithoutStuckCapture(t *testing.T) {
	ctx := context.Background()
	clock := newFakeScheduler(time.Now())
	capture := &fakeCapture{stopForever: make(chan struct{})}
	workflow, err := OpenWithAudioAndScheduler(ctx, t.TempDir(), fakeAudio{capture: capture}, clock)
	if err != nil {
		t.Fatalf("OpenWithAudioAndScheduler() error = %v", err)
	}
	workflow.limitStopTimeout = time.Millisecond
	t.Cleanup(func() { _ = workflow.Close() })
	if err := workflow.SelectAudioSource(ctx, audio.Source{ID: "speakers.monitor", Name: "Speakers"}, 1); err != nil {
		t.Fatalf("SelectAudioSource() error = %v", err)
	}
	if err := workflow.SetRecordingLimit(ctx, time.Minute); err != nil {
		t.Fatalf("SetRecordingLimit() error = %v", err)
	}
	if _, err := workflow.StartRecording(ctx); err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}
	clock.Advance(time.Minute)
	state := workflow.CaptureState()
	if state.Active != nil || state.Failure == "" || !strings.Contains(state.Failure, "deadline exceeded") {
		t.Errorf("CaptureState() after stalled automatic stop = %#v, want terminal timeout", state)
	}
	if _, err := os.Stat(capture.path); err != nil {
		t.Errorf("staged audio was not preserved after timed out stop: %v", err)
	}
}

func TestWorkflowRetainsInterruptedRecordingWhenRecoveryProbeFails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := recording.OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	finalPath := filepath.Join(dir, "recordings", "unprobed.opus")
	partialPath := strings.TrimSuffix(finalPath, ".opus") + ".partial.opus"
	if err := os.MkdirAll(filepath.Dir(partialPath), 0o700); err != nil {
		t.Fatalf("create recordings directory: %v", err)
	}
	if err := os.WriteFile(partialPath, []byte("opus"), 0o600); err != nil {
		t.Fatalf("create partial audio: %v", err)
	}
	if err := store.Save(ctx, recording.Recording{ID: "unprobed", Title: "Unprobed", StartedAt: time.Now(), AudioPath: finalPath, PendingPromotion: true, Interrupted: true}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	workflow, err := OpenWithAudio(ctx, dir, fakeAudio{playableErr: errors.New("ffprobe unavailable")})
	if err != nil {
		t.Fatalf("OpenWithAudio() error = %v, want recovery failure not to block startup", err)
	}
	t.Cleanup(func() { _ = workflow.Close() })
	startup, err := workflow.Startup(ctx)
	if err != nil || startup.RecoveryWarning == "" {
		t.Errorf("Startup() = %#v, %v; want recovery warning", startup, err)
	}
	history, err := workflow.History(ctx)
	if err != nil || len(history) != 1 || !history[0].PendingPromotion {
		t.Errorf("History() = %#v, %v; want retained pending Recording", history, err)
	}
	if _, err := os.Stat(partialPath); err != nil {
		t.Errorf("partial audio was not retained: %v", err)
	}
}

type fakeAudio struct {
	sources     []audio.Source
	err         error
	activity    float64
	capture     *fakeCapture
	playable    bool
	playableErr error
	startErr    error
	onStart     func(string)
}

func (f fakeAudio) Playable(context.Context, string) (bool, error) { return f.playable, f.playableErr }

func (f fakeAudio) Sources(context.Context) ([]audio.Source, error) {
	return f.sources, f.err
}

func (f fakeAudio) Activity(context.Context, audio.Source) (float64, error) {
	return f.activity, nil
}

func (f fakeAudio) Start(_ context.Context, source audio.Source, path string) (audio.Capture, error) {
	if f.onStart != nil {
		f.onStart(path)
	}
	if f.startErr != nil {
		return nil, f.startErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte("opus"), 0o600); err != nil {
		return nil, err
	}
	if f.capture == nil {
		return &fakeCapture{}, nil
	}
	f.capture.source = source
	f.capture.path = path
	return f.capture, nil
}

type fakeCapture struct {
	source               audio.Source
	stopped              bool
	path                 string
	stopErr              error
	waitErr              error
	waitUntilStopped     bool
	done                 chan struct{}
	stopDelay            time.Duration
	stopUntilContextDone bool
	stopForever          <-chan struct{}
}

type fakeScheduler struct {
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	action  func()
	stopped bool
}

func newFakeScheduler(now time.Time) *fakeScheduler { return &fakeScheduler{now: now} }

func (s *fakeScheduler) Now() time.Time { return s.now }

func (s *fakeScheduler) AfterFunc(after time.Duration, action func()) Timer {
	timer := &fakeTimer{at: s.now.Add(after), action: action}
	s.timers = append(s.timers, timer)
	return timer
}

func (s *fakeScheduler) Advance(after time.Duration) {
	s.now = s.now.Add(after)
	for _, timer := range s.timers {
		if !timer.stopped && !timer.at.After(s.now) {
			timer.stopped = true
			timer.action()
		}
	}
}

func (t *fakeTimer) Stop() bool {
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

func (c *fakeCapture) Stop(ctx context.Context) error {
	c.stopped = true
	if c.stopForever != nil {
		<-c.stopForever
	}
	if c.stopUntilContextDone {
		<-ctx.Done()
		return ctx.Err()
	}
	time.Sleep(c.stopDelay)
	if c.done != nil {
		close(c.done)
	}
	return c.stopErr
}

func (c *fakeCapture) Wait() error {
	if c.waitUntilStopped {
		if c.done == nil {
			c.done = make(chan struct{})
		}
		<-c.done
	}
	return c.waitErr
}
