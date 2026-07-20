package tui

import (
	"context"
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
	recordings []recording.Recording
	err        error
	sources    []audio.Source
	audioErr   error
	activity   float64
	metered    *[]audio.Source
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
