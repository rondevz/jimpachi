package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jimpachi/internal/app"
	"jimpachi/internal/audio"
	"jimpachi/internal/recording"
)

func TestModelShowsEmptyRecordingHistory(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{}))

	view := model.View()
	if !strings.Contains(view, "Recording history") {
		t.Errorf("View() = %q, want Recording history", view)
	}
	if !strings.Contains(view, "No recordings yet.") {
		t.Errorf("View() = %q, want empty history message", view)
	}
}

func TestModelShowsRecordingHistory(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{recordings: []recording.Recording{{
		ID:        "recording-1",
		Title:     "Deployment instructions",
		StartedAt: time.Date(2026, time.July, 20, 10, 30, 0, 0, time.UTC),
		Duration:  18 * time.Minute,
	}}}))

	view := model.View()
	if !strings.Contains(view, "Deployment instructions") {
		t.Errorf("View() = %q, want Recording title", view)
	}
	if !strings.Contains(view, "18m") {
		t.Errorf("View() = %q, want Recording duration", view)
	}
}

func TestModelShowsMissingAudioCondition(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{recordings: []recording.Recording{{Title: "Moved", AudioMissing: true}}}))
	if !strings.Contains(model.View(), "Audio file is missing.") {
		t.Errorf("View() = %q, want missing-audio condition", model.View())
	}
}

func TestModelHistoryLoadCanBeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	model := New(ctx, blockingHistory{started: started})
	result := make(chan tea.Msg, 1)
	go func() { result <- model.Init()() }()

	<-started
	cancel()

	select {
	case message := <-result:
		updated, _ := model.Update(message)
		if !strings.Contains(updated.View(), "context canceled") {
			t.Errorf("View() = %q, want cancellation error", updated.View())
		}
	case <-time.After(time.Second):
		t.Fatal("history load did not stop after cancellation")
	}
}

func TestModelLetsUserSelectAudioSourceAndShowsItsActivity(t *testing.T) {
	speakers := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	headphones := audio.Source{ID: "headphones.monitor", Name: "Headphones"}
	workflow := fakeHistory{sources: []audio.Source{speakers, headphones}, activity: 0.6}
	model := load(t, New(context.Background(), workflow))

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("selecting an Audio source did not issue a workflow command")
	}
	updated, command = updated.Update(command())
	model = updated.(Model)
	if command != nil {
		updated, _ = model.Update(command())
		model = updated.(Model)
	}

	if !strings.Contains(model.View(), "> Headphones [######----]") {
		t.Errorf("View() = %q, want highlighted source and activity meter", model.View())
	}
}

func TestModelShowsActivityOnlyForHighlightedAudioSource(t *testing.T) {
	speakers := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	headphones := audio.Source{ID: "headphones.monitor", Name: "Headphones"}
	model := load(t, New(context.Background(), fakeHistory{sources: []audio.Source{speakers, headphones}, activity: 0.6}))

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.Update(command())
	model = updated.(Model)
	view := model.View()
	if strings.Contains(view, "  Speakers [######----]") {
		t.Errorf("View() = %q, rendered headphone activity for unhighlighted speakers", view)
	}
	if !strings.Contains(view, "> Headphones [######----]") {
		t.Errorf("View() = %q, want highlighted headphone activity", view)
	}
}

func TestModelSanitizesAudioSourceNamesForTerminalRendering(t *testing.T) {
	source := audio.Source{
		ID:   "alsa_output.usb.monitor",
		Name: "\x1b]8;;https://example.invalid\aTrusted\x1b]8;;\a\x1b[31m Output\x1b[0m\n",
	}
	var metered []audio.Source
	model := load(t, New(context.Background(), fakeHistory{sources: []audio.Source{source}, metered: &metered}))

	view := model.View()
	if strings.ContainsAny(view, "\x1b\a") {
		t.Errorf("View() = %q, rendered terminal control characters from Audio source name", view)
	}
	if strings.Contains(view, "example.invalid") {
		t.Errorf("View() = %q, rendered an OSC hyperlink payload", view)
	}
	if !strings.Contains(view, "Trusted Output") {
		t.Errorf("View() = %q, want readable sanitized Audio source name", view)
	}
	_, command := model.Update(activityTick{sourceID: source.ID, generation: 0})
	if command == nil {
		t.Fatal("activity tick did not request the highlighted Audio source")
	}
	_ = command()
	if len(metered) != 1 || metered[0].ID != source.ID {
		t.Errorf("AudioActivity() sources = %#v, want raw source ID %q", metered, source.ID)
	}
}

func TestModelClearsActivityWhenHighlightedAudioSourceChanges(t *testing.T) {
	speakers := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	headphones := audio.Source{ID: "headphones.monitor", Name: "Headphones"}
	model := load(t, New(context.Background(), fakeHistory{sources: []audio.Source{speakers, headphones}}))

	updated, _ := model.Update(sourceActivity{sourceID: speakers.ID, generation: 0, activity: 0.6})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if strings.Contains(model.View(), "> Headphones [######----]") {
		t.Errorf("View() = %q, rendered prior source activity for a newly highlighted source", model.View())
	}
}

func TestModelClearsActivityWhenAudioSourceConfirmationStartsAndCompletes(t *testing.T) {
	source := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	model := load(t, New(context.Background(), fakeHistory{sources: []audio.Source{source}}))

	updated, _ := model.Update(sourceActivity{sourceID: source.ID, generation: 0, activity: 0.6})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if strings.Contains(model.View(), "[######----]") {
		t.Errorf("View() = %q, retained activity while source confirmation was pending", model.View())
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if strings.Contains(model.View(), "[######----]") {
		t.Errorf("View() = %q, retained activity after source confirmation", model.View())
	}
}

func TestModelKeepsConfirmedExplicitAudioSourceVisibleAndMetered(t *testing.T) {
	explicit := audio.Source{ID: "manual.monitor", Name: "manual.monitor", Explicit: true}
	model := load(t, New(context.Background(), fakeHistory{audioErr: context.DeadlineExceeded, activity: 0.4}))

	updated, command := model.Update(sourceSelected{source: explicit})
	if command == nil {
		t.Fatal("confirming an explicit Audio source did not start its activity meter")
	}
	updated, _ = updated.Update(command())
	model = updated.(Model)
	if !strings.Contains(model.View(), "> manual.monitor [####------]") {
		t.Errorf("View() = %q, want confirmed explicit source and its activity meter", model.View())
	}
}

func TestModelShowsCaptureDurationAndCompletedRecordingDetail(t *testing.T) {
	source := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	completed := recording.Recording{
		ID:        "recording-1",
		Title:     "Recording 2026-07-20 10:30",
		StartedAt: time.Date(2026, time.July, 20, 10, 30, 0, 0, time.UTC),
		Duration:  5 * time.Second,
		AudioPath: "/recordings/recording-1.opus",
	}
	workflow := fakeHistory{
		sources: []audio.Source{source},
		started: app.ActiveRecording{ID: completed.ID, StartedAt: time.Now().Add(-time.Second), Source: source},
		stopped: completed,
	}
	model := load(t, New(context.Background(), workflow))

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	updated, _ = updated.Update(command())
	model = updated.(Model)
	if !strings.Contains(model.View(), "RECORDING") || !strings.Contains(model.View(), "Press s to stop") {
		t.Errorf("View() = %q, want active Recording duration and stop control", model.View())
	}

	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	updated, _ = updated.Update(command())
	model = updated.(Model)
	for _, want := range []string{"Recording detail", completed.ID, completed.AudioPath, "Duration: 5s"} {
		if !strings.Contains(model.View(), want) {
			t.Errorf("View() = %q, want %q", model.View(), want)
		}
	}
}

func TestModelIgnoresDuplicateAndStaleRecordingOperations(t *testing.T) {
	source := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	completed := recording.Recording{ID: "recording-1", Title: "Completed"}
	workflow := fakeHistory{sources: []audio.Source{source}, started: app.ActiveRecording{ID: completed.ID, StartedAt: time.Now(), Source: source}, stopped: completed}
	model := load(t, New(context.Background(), workflow))

	updated, start := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	model = updated.(Model)
	if start == nil {
		t.Fatal("first start did not issue a command")
	}
	_, duplicateStart := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if duplicateStart != nil {
		t.Fatal("duplicate start issued another command")
	}
	updated, stop := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	model = updated.(Model)
	if stop != nil {
		t.Fatal("stop while start is pending issued an early stop command")
	}

	updated, stop = model.Update(start())
	model = updated.(Model)
	if stop == nil {
		t.Fatal("stop requested during start was not issued after start completed")
	}
	updated, _ = model.Update(recordingStopped{recording: completed, operation: 0})
	model = updated.(Model)
	if len(model.recordings) != 0 {
		t.Errorf("stale stop result added Recording history: %#v", model.recordings)
	}
	_, duplicateStop := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if duplicateStop != nil {
		t.Fatal("duplicate pending stop issued another command")
	}
	updated, _ = model.Update(stop())
	model = updated.(Model)
	if len(model.recordings) != 1 || model.recordings[0].ID != completed.ID {
		t.Errorf("completed stop result recordings = %#v, want %#v", model.recordings, completed)
	}
	_, duplicateStop = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if duplicateStop != nil {
		t.Fatal("duplicate stop issued another command")
	}
}

func TestModelSanitizesRecordingTitlesAndPaths(t *testing.T) {
	unsafe := "\x1b]8;;https://example.invalid\aTrusted\x1b]8;;\a\x1b[31m title\x1b[0m"
	path := "/recordings/\x1b[31mrecording.opus"
	model := load(t, New(context.Background(), fakeHistory{recordings: []recording.Recording{{Title: unsafe, AudioPath: path}}}))
	if view := model.View(); strings.ContainsAny(view, "\x1b\a") || strings.Contains(view, "example.invalid") {
		t.Errorf("history View() = %q, rendered Recording terminal control content", view)
	}
	model.detail = &recording.Recording{Title: unsafe, AudioPath: path}
	model.editingTitle = true
	model.title = unsafe
	view := model.View()
	if strings.ContainsAny(view, "\x1b\a") || strings.Contains(view, "example.invalid") {
		t.Errorf("View() = %q, rendered Recording terminal control content", view)
	}
	if !strings.Contains(view, "Trusted title") {
		t.Errorf("View() = %q, want sanitized editable Recording title", view)
	}
	model.editingTitle = false
	view = model.View()
	if strings.ContainsAny(view, "\x1b\a") || strings.Contains(view, "example.invalid") || !strings.Contains(view, "/recordings/recording.opus") {
		t.Errorf("View() = %q, want sanitized Recording detail title and path", view)
	}
}

func TestModelClearsActiveRecordingAfterStopFailure(t *testing.T) {
	model := Model{workflow: fakeHistory{}, active: &app.ActiveRecording{ID: "recording-1"}, stopPending: true, recordingOp: 1}
	updated, _ := model.Update(recordingStopped{operation: 1, err: errors.New("save failed")})
	model = updated.(Model)
	if model.active != nil {
		t.Error("active Recording remained after terminal stop failure")
	}
	if model.err == nil {
		t.Error("terminal stop failure was not shown")
	}
}

func TestModelRestoresRetryableCaptureStateAfterStopFailure(t *testing.T) {
	retryable := &app.ActiveRecording{ID: "recording-1"}
	model := Model{workflow: fakeHistory{captureState: app.CaptureState{Active: retryable}}, active: retryable, stopPending: true, recordingOp: 1}
	updated, _ := model.Update(recordingStopped{operation: 1, err: errors.New("promote failed")})
	model = updated.(Model)
	if model.active == nil || model.active.ID != retryable.ID {
		t.Errorf("active Recording = %#v, want retryable capture %#v", model.active, retryable)
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if command == nil {
		t.Fatal("retryable capture did not issue another stop command")
	}
	_ = updated
}

func TestModelClearsActiveRecordingWhenWorkflowReportsNoCaptureState(t *testing.T) {
	model := Model{workflow: fakeHistory{}, active: &app.ActiveRecording{ID: "recording-1"}}
	updated, _ := model.Update(recordingTick{})
	model = updated.(Model)
	if model.active != nil {
		t.Error("active Recording remained after workflow reported no capture state")
	}
	if model.err == nil {
		t.Error("missing workflow capture state was not shown as terminal failure")
	}
}

func TestModelDiscardsOutOfOrderAudioSourceConfirmation(t *testing.T) {
	speakers := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	headphones := audio.Source{ID: "headphones.monitor", Name: "Headphones"}
	model := load(t, New(context.Background(), fakeHistory{sources: []audio.Source{speakers, headphones}}))

	updated, first := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, second := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	secondMessage := second().(sourceSelected)
	updated, _ = model.Update(secondMessage)
	model = updated.(Model)
	firstMessage := first().(sourceSelected)
	updated, _ = model.Update(firstMessage)
	model = updated.(Model)

	if !strings.Contains(model.View(), "> Headphones") {
		t.Errorf("View() = %q, stale confirmation overrode the latest Audio source", model.View())
	}
}

func TestModelDiscardsActivityFromPreviouslyHighlightedAudioSource(t *testing.T) {
	speakers := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	headphones := audio.Source{ID: "headphones.monitor", Name: "Headphones"}
	model := load(t, New(context.Background(), fakeHistory{sources: []audio.Source{speakers, headphones}}))

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(sourceActivity{sourceID: speakers.ID, generation: 0, activity: 0.9})
	model = updated.(Model)

	if strings.Contains(model.View(), "> Headphones [#########-") {
		t.Errorf("View() = %q, stale speaker activity replaced the highlighted source meter", model.View())
	}
}

func TestModelDiscardsActivityTickFromPreviouslyHighlightedAudioSource(t *testing.T) {
	speakers := audio.Source{ID: "speakers.monitor", Name: "Speakers"}
	headphones := audio.Source{ID: "headphones.monitor", Name: "Headphones"}
	model := load(t, New(context.Background(), fakeHistory{sources: []audio.Source{speakers, headphones}}))

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	_, command := model.Update(activityTick{sourceID: speakers.ID, generation: 0})
	if command != nil {
		t.Fatal("stale Audio activity tick scheduled another meter command")
	}
}

func TestModelOffersManualSourcePathWhenDiscoveryFails(t *testing.T) {
	workflow := fakeHistory{audioErr: context.DeadlineExceeded}
	model := load(t, New(context.Background(), workflow))

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	model = updated.(Model)
	if !strings.Contains(model.View(), "Monitor source path: _") {
		t.Errorf("View() = %q, want explicit source path entry", model.View())
	}
}

func load(t *testing.T, model Model) Model {
	t.Helper()
	message := model.Init()()
	updated, _ := model.Update(message)
	return updated.(Model)
}

type fakeHistory struct {
	recordings   []recording.Recording
	err          error
	sources      []audio.Source
	audioErr     error
	activity     float64
	metered      *[]audio.Source
	started      app.ActiveRecording
	stopped      recording.Recording
	captureState app.CaptureState
}

func (f fakeHistory) Startup(context.Context) (app.Startup, error) { return app.Startup{}, nil }

func (f fakeHistory) AudioSources(context.Context) (app.AudioState, error) {
	return app.AudioState{Sources: f.sources}, f.audioErr
}

func (f fakeHistory) SelectAudioSource(context.Context, audio.Source, uint64) error { return nil }

func (f fakeHistory) AudioActivity(_ context.Context, source audio.Source) (float64, error) {
	if f.metered != nil {
		*f.metered = append(*f.metered, source)
	}
	return f.activity, nil
}

func (f fakeHistory) StartRecording(context.Context) (app.ActiveRecording, error) {
	return f.started, nil
}

func (f fakeHistory) StopRecording(context.Context) (recording.Recording, error) {
	return f.stopped, nil
}

func (f fakeHistory) RenameRecording(context.Context, string, string) error { return nil }

func (f fakeHistory) CaptureState() app.CaptureState { return f.captureState }

func (f fakeHistory) History(context.Context) ([]recording.Recording, error) {
	return f.recordings, f.err
}

type blockingHistory struct {
	started chan<- struct{}
}

func (f blockingHistory) History(ctx context.Context) ([]recording.Recording, error) {
	close(f.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f blockingHistory) Startup(context.Context) (app.Startup, error) { return app.Startup{}, nil }

func (f blockingHistory) AudioSources(context.Context) (app.AudioState, error) {
	return app.AudioState{}, nil
}

func (f blockingHistory) SelectAudioSource(context.Context, audio.Source, uint64) error { return nil }

func (f blockingHistory) AudioActivity(context.Context, audio.Source) (float64, error) { return 0, nil }

func (f blockingHistory) StartRecording(context.Context) (app.ActiveRecording, error) {
	return app.ActiveRecording{}, nil
}

func (f blockingHistory) StopRecording(context.Context) (recording.Recording, error) {
	return recording.Recording{}, nil
}

func (f blockingHistory) RenameRecording(context.Context, string, string) error { return nil }

func (f blockingHistory) CaptureState() app.CaptureState { return app.CaptureState{} }
