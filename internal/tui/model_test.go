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

func TestModelEditsAndSavesSettings(t *testing.T) {
	saved := app.Settings{}
	workflow := fakeHistory{settings: app.Settings{AudioSource: audio.Source{Name: "Speakers"}, RecordingLimit: time.Hour, AutomaticTranscription: true}, savedSettings: &saved}
	model := load(t, New(context.Background(), workflow))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	updated, _ = updated.Update(command())
	model = updated.(Model)
	if view := model.View(); !strings.Contains(view, "Settings") || !strings.Contains(view, "Audio source: Speakers") {
		t.Errorf("View() = %q, want Settings with selected Audio source", view)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	updated, command = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if command == nil {
		t.Fatal("saving Settings did not issue a workflow command")
	}
	updated, _ = updated.Update(command())
	if saved.AutomaticTranscription {
		t.Errorf("saved Settings = %#v, want automatic Transcription disabled", saved)
	}
}

func TestModelShowsSettingsValidationFeedback(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{settings: app.Settings{ValidationError: "whisper model path: no such file"}}))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	updated, _ = updated.Update(command())
	if view := updated.View(); !strings.Contains(view, "Configuration needs attention: whisper model path: no such file") {
		t.Errorf("View() = %q, want Settings validation feedback", view)
	}
}

func TestMeterShowsQuietNonZeroAudioActivity(t *testing.T) {
	if got, want := meter(0.01), "[#---------]"; got != want {
		t.Errorf("meter(0.01) = %q, want %q", got, want)
	}
}

func TestModelRequiresConfirmationBeforeDeletingRecording(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{}))
	model.detail = &recording.Recording{ID: "recording-1", Title: "Instructions"}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if command != nil {
		t.Fatal("delete request did not require confirmation")
	}
	model = updated.(Model)
	if !strings.Contains(model.View(), "Press y to confirm") {
		t.Errorf("View() = %q, want deletion confirmation", model.View())
	}
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if command == nil {
		t.Fatal("confirmed deletion did not call workflow")
	}
	if _, ok := command().(recordingDeleted); !ok {
		t.Fatalf("deletion message = %T", command())
	}
	_ = updated
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

func TestModelOpensPersistedRecordingDetailFromHistory(t *testing.T) {
	persisted := recording.Recording{ID: "recording-1", Title: "Deployment instructions", TranscriptionStatus: recording.TranscriptionSucceeded, Transcription: []recording.Segment{{Start: time.Second, End: 2 * time.Second, Text: "Deploy now."}}}
	model := load(t, New(context.Background(), fakeHistory{recordings: []recording.Recording{persisted}}))
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated, command := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("opening selected Recording did not request detail")
	}
	updated, _ = updated.Update(command())
	if view := updated.View(); !strings.Contains(view, "Recording detail") || !strings.Contains(view, "Deploy now.") {
		t.Errorf("View() = %q, want persisted Recording detail and Transcription", view)
	}
}

func TestModelPollsAfterManualTranscriptionRequestIsPending(t *testing.T) {
	pending := recording.Recording{ID: "recording-1", TranscriptionStatus: recording.TranscriptionPending}
	model := load(t, New(context.Background(), fakeHistory{requested: &pending}))
	model.detail = &recording.Recording{ID: pending.ID, TranscriptionStatus: recording.TranscriptionFailed}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if command == nil {
		t.Fatal("manual Transcription did not issue a request")
	}
	updated, poll := updated.Update(command())
	if poll == nil {
		t.Fatal("pending manual Transcription did not schedule polling")
	}
}

func TestModelShowsFullTimestampedTranscriptionInRecordingDetail(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{}))
	model.detail = &recording.Recording{Title: "Instructions", Transcription: []recording.Segment{
		{Start: time.Second, End: 3 * time.Second, Text: "Deploy the service."},
		{Start: 4 * time.Second, End: 6 * time.Second, Text: "Verify the dashboard."},
	}}
	view := model.View()
	for _, want := range []string{"Transcription", "[00:00:01 - 00:00:03] Deploy the service.", "[00:00:04 - 00:00:06] Verify the dashboard."} {
		if !strings.Contains(view, want) {
			t.Errorf("View() = %q, want %q", view, want)
		}
	}
}

func TestModelShowsStructuredSummaryAndRetryControl(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{}))
	model.detail = &recording.Recording{ID: "recording-1", TranscriptionStatus: recording.TranscriptionSucceeded, SummaryStatus: recording.TranscriptionFailed, SummaryError: "Summary could not be completed. Try again."}
	view := model.View()
	if !strings.Contains(view, "Summary failed: Summary could not be completed. Try again.") || !strings.Contains(view, "m to retry Summary") {
		t.Errorf("View() = %q", view)
	}
	model.detail.SummaryStatus = recording.TranscriptionSucceeded
	model.detail.Summary = recording.Summary{Overview: "Release Friday.", ActionItems: []string{"Test release"}, OpenQuestions: []string{"Who approves?"}}
	view = model.View()
	for _, want := range []string{"Release Friday.", "Action items: Test release", "Open questions: Who approves?"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() = %q, want %q", view, want)
		}
	}
}

func TestModelDoesNotOfferOrRequestSummaryBeforeTranscriptionSucceeds(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{}))
	model.detail = &recording.Recording{ID: "recording-1", TranscriptionStatus: recording.TranscriptionFailed}
	if strings.Contains(model.View(), "m to") {
		t.Errorf("View() = %q, offered Summary before Transcription", model.View())
	}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	if command != nil {
		t.Fatal("m requested a Summary before Transcription succeeded")
	}
}

func TestModelCancelsQueuedSummary(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{}))
	model.detail = &recording.Recording{ID: "recording-1", TranscriptionStatus: recording.TranscriptionSucceeded, SummaryStatus: recording.TranscriptionPending}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if command == nil {
		t.Fatal("cancellation key did not issue a Summary cancellation")
	}
	if _, ok := command().(transcriptionCancelled); !ok {
		t.Fatalf("cancellation command message = %T", command())
	}
}

func TestModelShowsFailedTranscriptionAndStopsPolling(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{}))
	model.detail = &recording.Recording{ID: "recording-1", TranscriptionStatus: recording.TranscriptionFailed, TranscriptionError: "Transcription could not be completed."}
	if view := model.View(); !strings.Contains(view, "Transcription failed: Transcription could not be completed.") || !strings.Contains(view, "Press t to retry") {
		t.Errorf("View() = %q, want Transcription failure and retry guidance", view)
	}
	updated, command := model.Update(transcriptionLoaded{recording: *model.detail})
	if command != nil {
		t.Fatal("terminal failed Transcription scheduled another poll")
	}
	_ = updated
}

func TestModelShowsQueuedProcessingAndOffersCancellation(t *testing.T) {
	pending := recording.Recording{ID: "recording-1", Title: "Instructions", TranscriptionStatus: recording.TranscriptionPending}
	model := load(t, New(context.Background(), fakeHistory{recordings: []recording.Recording{pending}}))
	if view := model.View(); !strings.Contains(view, "Post-processing: queued") {
		t.Errorf("View() = %q, want queued post-processing status", view)
	}
	model.detail = &pending
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if command == nil {
		t.Fatal("cancellation key did not issue a workflow command")
	}
	_ = updated
	if _, ok := command().(transcriptionCancelled); !ok {
		t.Fatalf("cancellation command message = %T, want transcriptionCancelled", command())
	}
}

func TestModelUpdatesHistoryStatusAfterManualRequestAndCancellation(t *testing.T) {
	queued := recording.Recording{ID: "recording-1", Title: "Instructions", TranscriptionStatus: recording.TranscriptionPending}
	workflow := fakeHistory{recordings: []recording.Recording{{ID: queued.ID, Title: queued.Title, TranscriptionStatus: recording.TranscriptionNotQueued}}, requested: &queued}
	model := load(t, New(context.Background(), workflow))
	model.detail = &recording.Recording{ID: queued.ID, TranscriptionStatus: recording.TranscriptionNotQueued}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	updated, _ = updated.Update(command())
	model = updated.(Model)
	if model.recordings[0].TranscriptionStatus != recording.TranscriptionPending {
		t.Errorf("history status after request = %q, want pending", model.recordings[0].TranscriptionStatus)
	}
	updated, _ = model.Update(transcriptionCancelled{id: queued.ID, recording: recording.Recording{ID: queued.ID, TranscriptionStatus: recording.TranscriptionCancelled}})
	model = updated.(Model)
	if model.recordings[0].TranscriptionStatus != recording.TranscriptionCancelled {
		t.Errorf("history status after cancellation = %q, want cancelled", model.recordings[0].TranscriptionStatus)
	}
}

func TestModelUsesQueuedStatusReturnedAfterAutomaticStopInHistory(t *testing.T) {
	completed := recording.Recording{ID: "recording-1", Title: "Instructions", TranscriptionStatus: recording.TranscriptionPending}
	model := Model{workflow: fakeHistory{}, stopPending: true, recordingOp: 1}
	updated, _ := model.Update(recordingStopped{recording: completed, operation: 1})
	model = updated.(Model)
	if len(model.recordings) != 1 || model.recordings[0].TranscriptionStatus != recording.TranscriptionPending {
		t.Errorf("history after automatic stop = %#v, want queued Recording", model.recordings)
	}
}

func TestModelShowsMissingAudioCondition(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{recordings: []recording.Recording{{Title: "Moved", AudioMissing: true}}}))
	if !strings.Contains(model.View(), "Audio file is missing.") {
		t.Errorf("View() = %q, want missing-audio condition", model.View())
	}
}

func TestModelShowsInterruptedRecording(t *testing.T) {
	model := load(t, New(context.Background(), fakeHistory{recordings: []recording.Recording{{Title: "Interrupted", Interrupted: true}}}))
	if !strings.Contains(model.View(), "Interrupted capture") {
		t.Errorf("View() = %q, want interrupted capture marker", model.View())
	}
}

func TestModelShowsRecoveryAndAutomaticStopFailures(t *testing.T) {
	active := &app.ActiveRecording{ID: "recording-1"}
	model := load(t, New(context.Background(), fakeHistory{startup: app.Startup{RecoveryWarning: "Could not verify interrupted Recording"}}))
	if !strings.Contains(model.View(), "Could not verify interrupted Recording") {
		t.Errorf("View() = %q, want recovery warning", model.View())
	}
	model.active = active
	model.workflow = fakeHistory{captureState: app.CaptureState{Failure: "context deadline exceeded"}}
	updated, _ := model.Update(recordingTick{})
	model = updated.(Model)
	if model.active != nil || !strings.Contains(model.View(), "Recording failed: context deadline exceeded") {
		t.Errorf("View() = %q, want automatic stop failure", updated.View())
	}
}

func TestModelLetsUserDisableRecordingLimit(t *testing.T) {
	savedLimit := time.Hour
	workflow := fakeHistory{limit: time.Hour, savedLimit: &savedLimit}
	model := load(t, New(context.Background(), workflow))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if command == nil {
		t.Fatal("limit toggle did not issue a workflow command")
	}
	updated, _ = updated.Update(command())
	model = updated.(Model)
	if savedLimit != 0 || !strings.Contains(model.View(), "Recording limit: disabled") {
		t.Errorf("limit toggle saved %v; View() = %q", savedLimit, model.View())
	}
}

func TestModelRollsBackRecordingLimitAfterTerminalSaveFailure(t *testing.T) {
	workflow := fakeHistory{limit: time.Hour, limitErr: errors.New("disk full")}
	model := load(t, New(context.Background(), workflow))
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	model = updated.(Model)
	if model.recordingLimit != 65*time.Minute {
		t.Fatalf("optimistic Recording limit = %v, want 65m", model.recordingLimit)
	}
	updated, next := model.Update(command())
	model = updated.(Model)
	if next != nil || model.recordingLimit != time.Hour {
		t.Errorf("terminal save failure left command %v and limit %v, want no command and 1h", next != nil, model.recordingLimit)
	}
}

func TestModelSerializesRapidRecordingLimitAdjustments(t *testing.T) {
	writes := []time.Duration{}
	workflow := fakeHistory{limit: time.Hour, limitWrites: &writes}
	model := load(t, New(context.Background(), workflow))
	updated, first := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	model = updated.(Model)
	updated, second := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	model = updated.(Model)
	if second != nil || model.recordingLimit != 70*time.Minute {
		t.Fatalf("rapid adjustments returned second command %v and limit %v, want queued 70m", second != nil, model.recordingLimit)
	}
	updated, next := model.Update(first())
	model = updated.(Model)
	if next == nil {
		t.Fatal("first save did not flush the latest intended limit")
	}
	_, _ = model.Update(next())
	if got, want := writes, []time.Duration{65 * time.Minute, 70 * time.Minute}; !sameDurations(got, want) {
		t.Errorf("SetRecordingLimit() calls = %v, want %v", got, want)
	}
}

func TestModelSerializesRecordingLimitToggleAfterAdjustment(t *testing.T) {
	writes := []time.Duration{}
	model := load(t, New(context.Background(), fakeHistory{limit: time.Hour, limitWrites: &writes}))
	updated, first := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	model = updated.(Model)
	updated, second := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	model = updated.(Model)
	if second != nil || model.recordingLimit != 0 {
		t.Fatalf("toggle returned second command %v and limit %v, want queued disabled limit", second != nil, model.recordingLimit)
	}
	updated, next := model.Update(first())
	if next == nil {
		t.Fatal("first save did not flush disabled limit")
	}
	_, _ = updated.Update(next())
	if got, want := writes, []time.Duration{65 * time.Minute, 0}; !sameDurations(got, want) {
		t.Errorf("SetRecordingLimit() calls = %v, want %v", got, want)
	}
}

func TestModelShowsRecordingLimitWarningAndAutomaticCompletion(t *testing.T) {
	completed := recording.Recording{ID: "recording-1", Title: "Timed recording", Duration: 10 * time.Minute}
	active := &app.ActiveRecording{ID: completed.ID, StartedAt: time.Now(), Source: audio.Source{Name: "Speakers"}}
	workflow := fakeHistory{captureState: app.CaptureState{Active: active, Warning: "Recording will stop in 5m0s."}}
	model := Model{workflow: workflow, active: active}
	updated, _ := model.Update(recordingTick{})
	model = updated.(Model)
	if !strings.Contains(model.View(), "Recording will stop in 5m0s.") {
		t.Errorf("View() = %q, want recording-limit warning", model.View())
	}
	workflow.captureState = app.CaptureState{Completed: &completed}
	model.workflow = workflow
	updated, _ = model.Update(recordingTick{})
	model = updated.(Model)
	if model.detail == nil || model.detail.ID != completed.ID {
		t.Errorf("automatic completion detail = %#v, want %#v", model.detail, completed)
	}
	if strings.Contains(model.View(), "Recording will stop") {
		t.Errorf("View() = %q, retained warning after automatic completion", model.View())
	}
}

func TestModelClearsRecordingLimitWarningWhenRecordingStartsOrStops(t *testing.T) {
	active := app.ActiveRecording{ID: "recording-1"}
	model := Model{workflow: fakeHistory{}, warning: "Recording will stop in 5m0s.", startPending: true}
	updated, _ := model.Update(recordingStarted{recording: active})
	model = updated.(Model)
	if model.warning != "" {
		t.Errorf("warning after Recording start = %q, want empty", model.warning)
	}
	model.warning = "Recording will stop in 5m0s."
	model.stopPending = true
	updated, _ = model.Update(recordingStopped{recording: recording.Recording{ID: active.ID}})
	model = updated.(Model)
	if model.warning != "" {
		t.Errorf("warning after Recording stop = %q, want empty", model.warning)
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

	if !strings.Contains(model.View(), "> Headphones [#######---]") {
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
	if strings.Contains(view, "  Speakers [#######---]") {
		t.Errorf("View() = %q, rendered headphone activity for unhighlighted speakers", view)
	}
	if !strings.Contains(view, "> Headphones [#######---]") {
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
	if strings.Contains(model.View(), "> Headphones [#######---]") {
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
	if strings.Contains(model.View(), "[#######---]") {
		t.Errorf("View() = %q, retained activity while source confirmation was pending", model.View())
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if strings.Contains(model.View(), "[#######---]") {
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
	if !strings.Contains(model.View(), "> manual.monitor [######----]") {
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
	recordings    []recording.Recording
	err           error
	sources       []audio.Source
	audioErr      error
	activity      float64
	metered       *[]audio.Source
	started       app.ActiveRecording
	stopped       recording.Recording
	captureState  app.CaptureState
	startup       app.Startup
	limit         time.Duration
	savedLimit    *time.Duration
	limitWrites   *[]time.Duration
	limitErr      error
	requested     *recording.Recording
	cancelled     *string
	settings      app.Settings
	savedSettings *app.Settings
}

func (f fakeHistory) Startup(context.Context) (app.Startup, error) { return f.startup, nil }

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

func (f fakeHistory) RecordingLimit(context.Context) (time.Duration, error) { return f.limit, nil }

func (f fakeHistory) SetRecordingLimit(_ context.Context, limit time.Duration) error {
	if f.savedLimit != nil {
		*f.savedLimit = limit
	}
	if f.limitWrites != nil {
		*f.limitWrites = append(*f.limitWrites, limit)
	}
	return f.limitErr
}

func (f fakeHistory) AutomaticTranscription(context.Context) (bool, error) { return true, nil }

func (f fakeHistory) SetAutomaticTranscription(context.Context, bool) error { return nil }

func (f fakeHistory) Recording(_ context.Context, id string) (recording.Recording, error) {
	for _, value := range f.recordings {
		if value.ID == id {
			return value, nil
		}
	}
	return recording.Recording{ID: id}, nil
}

func (f fakeHistory) RequestTranscription(_ context.Context, id string) (recording.Recording, error) {
	if f.requested != nil {
		return *f.requested, nil
	}
	return f.Recording(context.Background(), id)
}

func (f fakeHistory) RequestSummary(_ context.Context, id string) (recording.Recording, error) {
	return f.Recording(context.Background(), id)
}

func (f fakeHistory) CancelTranscription(_ context.Context, id string) error {
	if f.cancelled != nil {
		*f.cancelled = id
	}
	return nil
}

func (f fakeHistory) CancelSummary(context.Context, string) error { return nil }

func (f fakeHistory) OpenRecordingAudio(context.Context, string) error { return nil }

func (f fakeHistory) DeleteRecording(context.Context, string) error { return nil }

func (f fakeHistory) Settings(context.Context) (app.Settings, error) { return f.settings, nil }

func (f fakeHistory) SaveSettings(_ context.Context, settings app.Settings) error {
	if f.savedSettings != nil {
		*f.savedSettings = settings
	}
	return nil
}

func sameDurations(got, want []time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

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

func (f blockingHistory) RecordingLimit(context.Context) (time.Duration, error) {
	return time.Hour, nil
}

func (f blockingHistory) SetRecordingLimit(context.Context, time.Duration) error { return nil }

func (f blockingHistory) AutomaticTranscription(context.Context) (bool, error) { return true, nil }

func (f blockingHistory) SetAutomaticTranscription(context.Context, bool) error { return nil }

func (f blockingHistory) Recording(context.Context, string) (recording.Recording, error) {
	return recording.Recording{}, nil
}

func (f blockingHistory) RequestTranscription(context.Context, string) (recording.Recording, error) {
	return recording.Recording{}, nil
}

func (f blockingHistory) RequestSummary(context.Context, string) (recording.Recording, error) {
	return recording.Recording{}, nil
}

func (f blockingHistory) CancelTranscription(context.Context, string) error { return nil }

func (f blockingHistory) CancelSummary(context.Context, string) error { return nil }

func (f blockingHistory) OpenRecordingAudio(context.Context, string) error { return nil }

func (f blockingHistory) DeleteRecording(context.Context, string) error { return nil }

func (f blockingHistory) Settings(context.Context) (app.Settings, error) { return app.Settings{}, nil }

func (f blockingHistory) SaveSettings(context.Context, app.Settings) error { return nil }
