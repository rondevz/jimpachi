package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jimpachi/internal/app"
	"jimpachi/internal/audio"
	"jimpachi/internal/recording"
)

// Workflow exposes Jimpachi behavior to the terminal UI.
type Workflow interface {
	History(context.Context) ([]recording.Recording, error)
	Startup(context.Context) (app.Startup, error)
	AudioSources(context.Context) (app.AudioState, error)
	SelectAudioSource(context.Context, audio.Source, uint64) error
	AudioActivity(context.Context, audio.Source) (float64, error)
}

// Model renders Jimpachi's Recording history.
type Model struct {
	ctx            context.Context
	workflow       Workflow
	recordings     []recording.Recording
	err            error
	reminder       string
	sources        []audio.Source
	selected       audio.Source
	sourceIndex    int
	discoveryError string
	activity       float64
	activityErr    error
	generation     uint64
	confirmation   uint64
	canUsePath     bool
	enteringPath   bool
	path           string
}

// New creates a terminal model backed by Recording history.
func New(ctx context.Context, workflow Workflow) Model {
	return Model{ctx: ctx, workflow: workflow}
}

// Init loads Recording history when the terminal program starts.
func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		startup, startupErr := m.workflow.Startup(m.ctx)
		recordings, historyErr := m.workflow.History(m.ctx)
		sources, audioErr := m.workflow.AudioSources(m.ctx)
		return initialLoaded{startup: startup, startupErr: startupErr, recordings: recordings, historyErr: historyErr, audio: sources, audioErr: audioErr}
	}
}

// Update applies user input and Recording-history results.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case initialLoaded:
		m.recordings = message.recordings
		m.err = message.historyErr
		m.reminder = message.startup.Reminder
		m.sources = message.audio.Sources
		m.selected = message.audio.Selected
		m.discoveryError = message.audio.DiscoveryError
		m.canUsePath = message.audio.CanUseExplicitPath
		for index, source := range m.sources {
			if source.ID == m.selected.ID {
				m.sourceIndex = index
				break
			}
		}
		if message.startupErr != nil && m.err == nil {
			m.err = message.startupErr
		}
		if message.audioErr != nil {
			m.discoveryError = message.audioErr.Error()
			m.canUsePath = len(m.sources) == 0
		}
		return m, m.activityCommand()
	case sourceActivity:
		if len(m.sources) == 0 || message.sourceID != m.sources[m.sourceIndex].ID || message.generation != m.generation {
			return m, nil
		}
		m.activity = message.activity
		m.activityErr = message.err
		return m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
			return activityTick{sourceID: message.sourceID, generation: message.generation}
		})
	case sourceSelected:
		if message.confirmation != m.confirmation {
			return m, nil
		}
		m.clearActivity()
		if message.err != nil {
			m.discoveryError = message.err.Error()
			return m, nil
		}
		m.selected = message.source
		m.generation++
		found := false
		for index, source := range m.sources {
			if source.ID == message.source.ID {
				m.sourceIndex = index
				found = true
				break
			}
		}
		if !found {
			m.sources = append([]audio.Source{message.source}, m.sources...)
			m.sourceIndex = 0
		}
		m.enteringPath = false
		return m, m.activityCommand()
	case activityTick:
		if len(m.sources) == 0 || message.sourceID != m.sources[m.sourceIndex].ID || message.generation != m.generation {
			return m, nil
		}
		return m, m.activityCommand()
	case tea.KeyMsg:
		key := message.String()
		if key == "q" || key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.enteringPath {
			switch key {
			case "esc":
				m.enteringPath = false
			case "enter":
				if m.path != "" {
					m.confirmation++
					m.generation++
					m.clearActivity()
					return m, m.selectSource(audio.Source{ID: m.path, Name: m.path, Explicit: true}, m.confirmation)
				}
			case "backspace":
				if len(m.path) > 0 {
					m.path = m.path[:len(m.path)-1]
				}
			default:
				if len(key) == 1 {
					m.path += key
				}
			}
			return m, nil
		}
		switch key {
		case "up", "k":
			if m.sourceIndex > 0 {
				m.sourceIndex--
				m.generation++
				m.clearActivity()
				return m, m.activityCommand()
			}
		case "down", "j":
			if m.sourceIndex+1 < len(m.sources) {
				m.sourceIndex++
				m.generation++
				m.clearActivity()
				return m, m.activityCommand()
			}
		case "enter":
			if len(m.sources) > 0 {
				m.confirmation++
				m.generation++
				m.clearActivity()
				return m, m.selectSource(m.sources[m.sourceIndex], m.confirmation)
			}
		case "a":
			if m.canUsePath {
				m.enteringPath = true
				m.path = ""
			}
		}
	}

	return m, nil
}

// View renders Recording history.
func (m Model) View() string {
	var view strings.Builder
	view.WriteString("Jimpachi\n\n")
	if m.reminder != "" {
		fmt.Fprintf(&view, "%s\n\n", m.reminder)
	}
	view.WriteString("Audio source\n\n")
	if m.discoveryError != "" {
		fmt.Fprintf(&view, "%s\n", m.discoveryError)
	}
	if m.enteringPath {
		fmt.Fprintf(&view, "Monitor source path: %s_\n\n", m.path)
	} else if len(m.sources) == 0 {
		view.WriteString("No system-output monitors found.\n\n")
	} else {
		for index, source := range m.sources {
			prefix := "  "
			if index == m.sourceIndex {
				prefix = "> "
			}
			if index == m.sourceIndex {
				fmt.Fprintf(&view, "%s%s %s\n", prefix, terminalText(source.Name), meter(m.activity))
			} else {
				fmt.Fprintf(&view, "%s%s\n", prefix, terminalText(source.Name))
			}
		}
		view.WriteString("\nUse up/down and enter to select.\n\n")
	}
	if m.activityErr != nil {
		fmt.Fprintf(&view, "Unable to measure Audio activity: %v\n\n", m.activityErr)
	}
	view.WriteString("Recording history\n\n")

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

type initialLoaded struct {
	recordings []recording.Recording
	historyErr error
	startup    app.Startup
	startupErr error
	audio      app.AudioState
	audioErr   error
}

type sourceActivity struct {
	sourceID   string
	generation uint64
	activity   float64
	err        error
}
type activityTick struct {
	sourceID   string
	generation uint64
}
type sourceSelected struct {
	source       audio.Source
	confirmation uint64
	err          error
}

func (m Model) activityCommand() tea.Cmd {
	if len(m.sources) == 0 {
		return nil
	}
	source := m.sources[m.sourceIndex]
	generation := m.generation
	return func() tea.Msg {
		activity, err := m.workflow.AudioActivity(m.ctx, source)
		return sourceActivity{sourceID: source.ID, generation: generation, activity: activity, err: err}
	}
}

func (m Model) selectSource(source audio.Source, confirmation uint64) tea.Cmd {
	return func() tea.Msg {
		err := m.workflow.SelectAudioSource(m.ctx, source, confirmation)
		return sourceSelected{source: source, confirmation: confirmation, err: err}
	}
}

func (m *Model) clearActivity() {
	m.activity = 0
	m.activityErr = nil
}

func meter(activity float64) string {
	filled := int(activity * 10)
	if filled > 10 {
		filled = 10
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", 10-filled) + "]"
}

func terminalText(text string) string {
	var sanitized strings.Builder
	for index := 0; index < len(text); {
		if text[index] != '\x1b' {
			sanitized.WriteByte(text[index])
			index++
			continue
		}
		index++
		if index == len(text) {
			break
		}
		switch text[index] {
		case '[':
			index++
			for index < len(text) {
				if text[index] >= 0x40 && text[index] <= 0x7e {
					index++
					break
				}
				index++
			}
		case ']':
			index++
			for index < len(text) {
				if text[index] == '\a' {
					index++
					break
				}
				if text[index] == '\x1b' && index+1 < len(text) && text[index+1] == '\\' {
					index += 2
					break
				}
				index++
			}
		default:
			index++
		}
	}

	return strings.Map(func(character rune) rune {
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) {
			return -1
		}
		return character
	}, sanitized.String())
}
