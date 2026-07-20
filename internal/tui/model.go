package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"jimpachi/internal/recording"
)

// History loads Recordings for display in the terminal UI.
type History interface {
	History(context.Context) ([]recording.Recording, error)
}

// Model renders Jimpachi's Recording history.
type Model struct {
	ctx        context.Context
	history    History
	recordings []recording.Recording
	err        error
}

// New creates a terminal model backed by Recording history.
func New(ctx context.Context, history History) Model {
	return Model{ctx: ctx, history: history}
}

// Init loads Recording history when the terminal program starts.
func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		recordings, err := m.history.History(m.ctx)
		return historyLoaded{recordings: recordings, err: err}
	}
}

// Update applies user input and Recording-history results.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case historyLoaded:
		m.recordings = message.recordings
		m.err = message.err
	case tea.KeyMsg:
		if key := message.String(); key == "q" || key == "ctrl+c" {
			return m, tea.Quit
		}
	}

	return m, nil
}

// View renders Recording history.
func (m Model) View() string {
	var view strings.Builder
	view.WriteString("Jimpachi\n\nRecording history\n\n")

	if m.err != nil {
		fmt.Fprintf(&view, "Unable to load Recording history: %v\n", m.err)
		return view.String()
	}
	if len(m.recordings) == 0 {
		view.WriteString("No recordings yet.\n")
		return view.String()
	}

	for _, recording := range m.recordings {
		fmt.Fprintf(&view, "%s\n", recording.Title)
		fmt.Fprintf(&view, "%s  %s\n\n", recording.StartedAt.Local().Format("2006-01-02 15:04"), recording.Duration.Round(1e9))
	}

	return view.String()
}

type historyLoaded struct {
	recordings []recording.Recording
	err        error
}
