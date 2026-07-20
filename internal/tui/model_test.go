package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

func load(t *testing.T, model Model) Model {
	t.Helper()
	message := model.Init()()
	updated, _ := model.Update(message)
	return updated.(Model)
}

type fakeHistory struct {
	recordings []recording.Recording
	err        error
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
