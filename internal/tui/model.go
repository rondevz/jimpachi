package tui

import (
	"context"
	"fmt"
	"math"
	"strconv"
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
	StartRecording(context.Context) (app.ActiveRecording, error)
	StopRecording(context.Context) (recording.Recording, error)
	RenameRecording(context.Context, string, string) error
	CaptureState() app.CaptureState
	RecordingLimit(context.Context) (time.Duration, error)
	SetRecordingLimit(context.Context, time.Duration) error
	AutomaticTranscription(context.Context) (bool, error)
	SetAutomaticTranscription(context.Context, bool) error
	Recording(context.Context, string) (recording.Recording, error)
	RequestTranscription(context.Context, string) (recording.Recording, error)
	RequestSummary(context.Context, string) (recording.Recording, error)
	CancelTranscription(context.Context, string) error
	CancelSummary(context.Context, string) error
	OpenRecordingAudio(context.Context, string) error
	DeleteRecording(context.Context, string) error
	Settings(context.Context) (app.Settings, error)
	SaveSettings(context.Context, app.Settings) error
}

// Model renders Jimpachi's Recording history.
type Model struct {
	ctx             context.Context
	workflow        Workflow
	recordings      []recording.Recording
	err             error
	reminder        string
	recoveryWarning string
	sources         []audio.Source
	selected        audio.Source
	sourceIndex     int
	discoveryError  string
	activity        float64
	activityErr     error
	generation      uint64
	confirmation    uint64
	canUsePath      bool
	enteringPath    bool
	path            string
	active          *app.ActiveRecording
	detail          *recording.Recording
	editingTitle    bool
	title           string
	startPending    bool
	stopPending     bool
	stopAfterStart  bool
	recordingOp     uint64
	recordingLimit  time.Duration
	persistedLimit  time.Duration
	limitSaving     bool
	savingLimit     time.Duration
	warning         string
	transcribing    bool
	summarizing     bool
	cancelling      bool
	automatic       bool
	historyFocused  bool
	historyIndex    int
	settings        app.Settings
	showSettings    bool
	settingsEditing string
	settingsInput   string
	settingsSaving  bool
	confirmDeletion bool
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
		limit, limitErr := m.workflow.RecordingLimit(m.ctx)
		automatic, automaticErr := m.workflow.AutomaticTranscription(m.ctx)
		return initialLoaded{startup: startup, startupErr: startupErr, recordings: recordings, historyErr: historyErr, audio: sources, audioErr: audioErr, limit: limit, limitErr: limitErr, automatic: automatic, automaticErr: automaticErr}
	}
}

// Update applies user input and Recording-history results.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case initialLoaded:
		m.recordings = message.recordings
		m.err = message.historyErr
		m.reminder = message.startup.Reminder
		m.recoveryWarning = message.startup.RecoveryWarning
		m.sources = message.audio.Sources
		m.selected = message.audio.Selected
		m.discoveryError = message.audio.DiscoveryError
		m.canUsePath = message.audio.CanUseExplicitPath
		m.recordingLimit = message.limit
		m.persistedLimit = message.limit
		m.automatic = message.automatic
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
		if message.limitErr != nil && m.err == nil {
			m.err = message.limitErr
		}
		if message.automaticErr != nil && m.err == nil {
			m.err = message.automaticErr
		}
		return m, tea.Batch(m.activityCommand(), m.processingTick())
	case recordingLimitSaved:
		if !m.limitSaving || message.limit != m.savingLimit {
			return m, nil
		}
		m.limitSaving = false
		if message.err != nil {
			m.err = message.err
			if m.recordingLimit != message.limit {
				return m, m.queueRecordingLimit(m.recordingLimit)
			}
			m.recordingLimit = m.persistedLimit
			return m, nil
		}
		m.persistedLimit = message.limit
		if m.recordingLimit != message.limit {
			return m, m.queueRecordingLimit(m.recordingLimit)
		}
		return m, nil
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
	case recordingStarted:
		if message.operation != m.recordingOp || !m.startPending {
			return m, nil
		}
		m.startPending = false
		m.warning = ""
		if message.err != nil {
			m.stopAfterStart = false
			m.err = message.err
			return m, nil
		}
		m.active = &message.recording
		for index, source := range m.sources {
			if source.ID == message.recording.Source.ID {
				m.sourceIndex = index
				break
			}
		}
		m.generation++
		m.clearActivity()
		if m.stopAfterStart {
			m.stopAfterStart = false
			m.stopPending = true
			return m, m.stopRecording(m.recordingOp)
		}
		return m, tea.Batch(m.activityCommand(), tea.Tick(time.Second, func(time.Time) tea.Msg { return recordingTick{} }))
	case recordingStopped:
		if message.operation != m.recordingOp || !m.stopPending {
			return m, nil
		}
		m.stopPending = false
		m.active = nil
		m.warning = ""
		if message.err != nil {
			if state := m.workflow.CaptureState(); state.Active != nil {
				m.active = state.Active
			}
			m.err = message.err
			return m, nil
		}
		m.detail = &message.recording
		m.recordings = append([]recording.Recording{message.recording}, m.recordings...)
		return m, m.transcriptionTick()
	case transcriptionLoaded:
		if m.detail == nil || m.detail.ID != message.recording.ID {
			return m, nil
		}
		if message.err == nil {
			m.detail = &message.recording
		}
		if ((m.detail.TranscriptionStatus == recording.TranscriptionPending || m.detail.TranscriptionStatus == recording.TranscriptionRunning) && !m.transcribing) || ((m.detail.SummaryStatus == recording.TranscriptionPending || m.detail.SummaryStatus == recording.TranscriptionRunning) && !m.summarizing) {
			return m, m.transcriptionTick()
		}
		return m, nil
	case processingLoaded:
		if message.err == nil {
			m.recordings = message.recordings
		}
		if hasActiveProcessing(m.recordings) {
			return m, m.processingTick()
		}
		return m, nil
	case transcriptionRequested:
		m.transcribing = false
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.detail = &message.recording
		m.replaceHistoryRecording(message.recording)
		if message.recording.TranscriptionStatus == recording.TranscriptionPending || message.recording.TranscriptionStatus == recording.TranscriptionRunning || message.recording.SummaryStatus == recording.TranscriptionPending || message.recording.SummaryStatus == recording.TranscriptionRunning {
			return m, m.transcriptionTick()
		}
		return m, nil
	case summaryRequested:
		m.summarizing = false
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.detail = &message.recording
		m.replaceHistoryRecording(message.recording)
		return m, m.transcriptionTick()
	case transcriptionCancelled:
		m.cancelling = false
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.replaceHistoryRecording(message.recording)
		if m.detail != nil && m.detail.ID == message.id {
			m.detail = &message.recording
			return m, m.transcriptionTick()
		}
		return m, nil
	case recordingOpened:
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.detail = &message.recording
		if message.recording.TranscriptionStatus == recording.TranscriptionPending || message.recording.TranscriptionStatus == recording.TranscriptionRunning || message.recording.SummaryStatus == recording.TranscriptionPending || message.recording.SummaryStatus == recording.TranscriptionRunning {
			return m, m.transcriptionTick()
		}
		return m, nil
	case automaticTranscriptionSaved:
		if message.err != nil {
			m.automatic = !message.enabled
			m.err = message.err
		}
		return m, nil
	case settingsLoaded:
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.settings = message.settings
		m.err = nil
		m.showSettings = true
		return m, nil
	case settingsSaved:
		m.settingsSaving = false
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.showSettings = false
		m.err = nil
		m.recordingLimit = message.settings.RecordingLimit
		m.persistedLimit = message.settings.RecordingLimit
		m.automatic = message.settings.AutomaticTranscription
		return m, nil
	case recordingDeleted:
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.confirmDeletion = false
		m.detail = nil
		for index := range m.recordings {
			if m.recordings[index].ID == message.id {
				m.recordings = append(m.recordings[:index], m.recordings[index+1:]...)
				break
			}
		}
		return m, nil
	case recordingAudioOpened:
		if message.err != nil {
			m.err = message.err
		}
		return m, nil
	case recordingTick:
		state := m.workflow.CaptureState()
		if state.Completed != nil {
			m.active = nil
			m.stopPending = false
			m.warning = ""
			m.detail = state.Completed
			m.recordings = append([]recording.Recording{*state.Completed}, m.recordings...)
			return m, m.transcriptionTick()
		}
		m.warning = state.Warning
		if state.Failure != "" {
			m.active = nil
			m.startPending = false
			m.stopPending = false
			m.stopAfterStart = false
			m.warning = ""
			m.err = fmt.Errorf("Recording failed: %s", state.Failure)
			return m, nil
		}
		if state.Active != nil {
			m.active = state.Active
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return recordingTick{} })
		}
		if m.active != nil || m.stopPending {
			m.active = nil
			m.stopPending = false
			m.startPending = false
			m.stopAfterStart = false
			m.warning = ""
			m.err = fmt.Errorf("Recording ended without a completed result")
		}
		return m, nil
	case recordingRenamed:
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		if m.detail != nil {
			m.detail.Title = message.title
		}
		for index := range m.recordings {
			if m.recordings[index].ID == message.id {
				m.recordings[index].Title = message.title
			}
		}
		m.editingTitle = false
		return m, nil
	case tea.KeyMsg:
		key := message.String()
		if key == "q" || key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.editingTitle {
			switch key {
			case "esc":
				m.editingTitle = false
			case "enter":
				if m.detail != nil && m.title != "" {
					return m, m.renameRecording(m.detail.ID, m.title)
				}
			case "backspace":
				if len(m.title) > 0 {
					m.title = m.title[:len(m.title)-1]
				}
			default:
				if len(key) == 1 {
					m.title += key
				}
			}
			return m, nil
		}
		if m.confirmDeletion {
			switch key {
			case "y":
				if m.detail != nil {
					return m, m.deleteRecording(m.detail.ID)
				}
			case "n", "esc":
				m.confirmDeletion = false
			}
			return m, nil
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
		if m.settingsEditing != "" {
			switch key {
			case "esc":
				m.settingsEditing, m.settingsInput = "", ""
			case "enter":
				m.applySettingsInput()
				m.settingsEditing, m.settingsInput = "", ""
			case "backspace":
				if len(m.settingsInput) > 0 {
					m.settingsInput = m.settingsInput[:len(m.settingsInput)-1]
				}
			default:
				if len(key) == 1 {
					m.settingsInput += key
				}
			}
			return m, nil
		}
		if m.showSettings {
			switch key {
			case "esc", "g":
				m.showSettings = false
			case "a":
				m.settings.AutomaticTranscription = !m.settings.AutomaticTranscription
			case "l":
				if m.settings.RecordingLimit == 0 {
					m.settings.RecordingLimit = time.Hour
				} else {
					m.settings.RecordingLimit = 0
				}
			case "[":
				if m.settings.RecordingLimit > 5*time.Minute {
					m.settings.RecordingLimit -= 5 * time.Minute
				}
			case "]":
				if m.settings.RecordingLimit == 0 {
					m.settings.RecordingLimit = time.Hour
				} else {
					m.settings.RecordingLimit += 5 * time.Minute
				}
			case "w", "m", "t", "o", "n":
				m.startSettingsEdit(key)
			case "s":
				if !m.settingsSaving {
					m.settingsSaving = true
					return m, m.saveSettings(m.settings)
				}
			}
			return m, nil
		}
		switch key {
		case "r":
			if m.active == nil && !m.startPending && !m.stopPending {
				m.recordingOp++
				m.startPending = true
				m.warning = ""
				return m, m.startRecording(m.recordingOp)
			}
		case "s":
			if m.startPending {
				m.stopAfterStart = true
			} else if m.active != nil && !m.stopPending {
				m.stopPending = true
				return m, m.stopRecording(m.recordingOp)
			}
		case "e":
			if m.detail != nil {
				m.editingTitle = true
				m.title = m.detail.Title
			}
		case "o":
			if m.detail != nil {
				return m, m.openRecordingAudio(m.detail.ID)
			}
		case "d":
			if m.detail != nil {
				m.confirmDeletion = true
			}
		case "t":
			if m.detail != nil && !m.transcribing {
				m.transcribing = true
				return m, m.requestTranscription(m.detail.ID)
			}
		case "m":
			if m.detail != nil && m.detail.TranscriptionStatus == recording.TranscriptionSucceeded && !m.summarizing && m.detail.SummaryStatus != recording.TranscriptionPending && m.detail.SummaryStatus != recording.TranscriptionRunning {
				m.summarizing = true
				return m, m.requestSummary(m.detail.ID)
			}
		case "c":
			if m.detail != nil && !m.cancelling && (m.detail.TranscriptionStatus == recording.TranscriptionPending || m.detail.TranscriptionStatus == recording.TranscriptionRunning) {
				m.cancelling = true
				return m, m.cancelTranscription(m.detail.ID)
			}
			if m.detail != nil && m.detail.TranscriptionStatus == recording.TranscriptionSucceeded && !m.cancelling && (m.detail.SummaryStatus == recording.TranscriptionPending || m.detail.SummaryStatus == recording.TranscriptionRunning) {
				m.cancelling = true
				return m, m.cancelSummary(m.detail.ID)
			}
		case "p":
			if m.active == nil && !m.startPending && !m.stopPending {
				m.automatic = !m.automatic
				return m, m.setAutomaticTranscription(m.automatic)
			}
		case "g":
			return m, m.loadSettings()
		case "esc":
			m.detail = nil
			m.historyFocused = true
		case "tab":
			if m.detail == nil && len(m.recordings) > 0 {
				m.historyFocused = !m.historyFocused
			}
		case "up", "k":
			if m.historyFocused && m.historyIndex > 0 {
				m.historyIndex--
			} else if m.active == nil && m.sourceIndex > 0 {
				m.sourceIndex--
				m.generation++
				m.clearActivity()
				return m, m.activityCommand()
			}
		case "down", "j":
			if m.historyFocused && m.historyIndex+1 < len(m.recordings) {
				m.historyIndex++
			} else if m.active == nil && m.sourceIndex+1 < len(m.sources) {
				m.sourceIndex++
				m.generation++
				m.clearActivity()
				return m, m.activityCommand()
			}
		case "enter":
			if m.historyFocused && len(m.recordings) > 0 {
				return m, m.openRecording(m.recordings[m.historyIndex].ID)
			} else if m.active == nil && len(m.sources) > 0 {
				m.confirmation++
				m.generation++
				m.clearActivity()
				return m, m.selectSource(m.sources[m.sourceIndex], m.confirmation)
			}
		case "a":
			if m.active == nil && m.canUsePath {
				m.enteringPath = true
				m.path = ""
			}
		case "l":
			if m.active == nil && !m.startPending && !m.stopPending {
				limit := time.Hour
				if m.recordingLimit > 0 {
					limit = 0
				}
				return m, m.queueRecordingLimit(limit)
			}
		case "[":
			if m.active == nil && !m.startPending && !m.stopPending && m.recordingLimit > 5*time.Minute {
				return m, m.queueRecordingLimit(m.recordingLimit - 5*time.Minute)
			}
		case "]":
			if m.active == nil && !m.startPending && !m.stopPending {
				limit := m.recordingLimit + 5*time.Minute
				if m.recordingLimit == 0 {
					limit = time.Hour
				}
				return m, m.queueRecordingLimit(limit)
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
	if m.recoveryWarning != "" {
		fmt.Fprintf(&view, "%s\n\n", m.recoveryWarning)
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
	if m.active != nil {
		fmt.Fprintf(&view, "RECORDING  %s  %s %s\n", time.Since(m.active.StartedAt).Round(time.Second), terminalText(m.active.Source.Name), meter(m.activity))
		if m.warning != "" {
			fmt.Fprintf(&view, "%s\n", m.warning)
		}
		view.WriteString("Press s to stop capture.\n\n")
	} else {
		view.WriteString("Press r to start capture from the selected source.\n\n")
	}
	if m.recordingLimit == 0 {
		view.WriteString("Recording limit: disabled. Press l to enable.\n\n")
	} else {
		fmt.Fprintf(&view, "Recording limit: %s. Press [ or ] to adjust; l disables.\n\n", m.recordingLimit)
	}
	if m.automatic {
		view.WriteString("Automatic transcription: enabled. Press p to disable.\n\n")
	} else {
		view.WriteString("Automatic transcription: disabled. Press p to enable.\n\n")
	}
	if m.detail != nil {
		view.WriteString("Recording detail\n\n")
		if m.editingTitle {
			fmt.Fprintf(&view, "Title: %s_\n", terminalText(m.title))
			view.WriteString("Enter saves title; esc cancels.\n")
		} else {
			fmt.Fprintf(&view, "%s\n", terminalText(m.detail.Title))
			fmt.Fprintf(&view, "ID: %s\nStarted: %s\nDuration: %s\nAudio: %s\n", terminalText(m.detail.ID), m.detail.StartedAt.Local().Format("2006-01-02 15:04:05"), m.detail.Duration.Round(time.Second), terminalText(m.detail.AudioPath))
			if m.detail.AudioMissing {
				view.WriteString("Audio file is missing.\n")
			}
			if m.detail.Interrupted {
				view.WriteString("Interrupted capture recovered after restart.\n")
			}
			if m.confirmDeletion {
				view.WriteString("\nDelete this Recording and all local artifacts? Press y to confirm or n to cancel.\n")
				return view.String()
			}
			view.WriteString("\nTranscription\n\n")
			switch m.detail.TranscriptionStatus {
			case recording.TranscriptionPending:
				view.WriteString("Post-processing: queued.\n")
			case recording.TranscriptionRunning:
				view.WriteString("Post-processing: transcribing.\n")
			case recording.TranscriptionFailed:
				fmt.Fprintf(&view, "Transcription failed: %s\nPost-processing: failed (%s).\n", terminalText(m.detail.TranscriptionError), terminalText(string(m.detail.TranscriptionFailureCategory)))
			case recording.TranscriptionCancelled:
				fmt.Fprintf(&view, "Post-processing: cancelled. %s\n", terminalText(m.detail.TranscriptionError))
			case recording.TranscriptionSucceeded:
				if len(m.detail.Transcription) == 0 {
					view.WriteString("No speech was detected.\n")
				}
			default:
				view.WriteString("No Transcription yet.\n")
			}
			if len(m.detail.Transcription) > 0 {
				for _, segment := range m.detail.Transcription {
					fmt.Fprintf(&view, "[%s - %s] %s\n", formatTimestamp(segment.Start), formatTimestamp(segment.End), terminalText(segment.Text))
				}
			}
			view.WriteString("\nSummary\n\n")
			switch m.detail.SummaryStatus {
			case recording.TranscriptionPending:
				view.WriteString("Post-processing: summary queued.\n")
			case recording.TranscriptionRunning:
				view.WriteString("Post-processing: summarizing.\n")
			case recording.TranscriptionFailed:
				fmt.Fprintf(&view, "Summary failed: %s\n", terminalText(m.detail.SummaryError))
			case recording.TranscriptionSucceeded:
				if m.detail.Summary.Overview != "" {
					fmt.Fprintf(&view, "%s\n", terminalText(m.detail.Summary.Overview))
				}
				for _, section := range []struct {
					name   string
					values []string
				}{{"Agreements and decisions", m.detail.Summary.Agreements}, {"Action items", m.detail.Summary.ActionItems}, {"Deadlines", m.detail.Summary.Deadlines}, {"Open questions", m.detail.Summary.OpenQuestions}} {
					if len(section.values) > 0 {
						fmt.Fprintf(&view, "%s: %s\n", section.name, terminalText(strings.Join(section.values, "; ")))
					}
				}
			default:
				view.WriteString("No Summary yet.\n")
			}
			if m.detail.TranscriptionStatus == recording.TranscriptionFailed || m.detail.TranscriptionStatus == recording.TranscriptionCancelled {
				view.WriteString("\nPress t to retry transcription; o to open audio; d to delete; e to edit title; esc returns to history.\n")
			} else if m.detail.TranscriptionStatus == recording.TranscriptionPending || m.detail.TranscriptionStatus == recording.TranscriptionRunning {
				view.WriteString("\nPress c to cancel; o to open audio; d to delete; e to edit title; esc returns to history.\n")
			} else if m.detail.TranscriptionStatus == recording.TranscriptionSucceeded && (m.detail.SummaryStatus == recording.TranscriptionPending || m.detail.SummaryStatus == recording.TranscriptionRunning) {
				view.WriteString("\nPress c to cancel Summary; o to open audio; d to delete; e to edit title; esc returns to history.\n")
			} else if m.detail.TranscriptionStatus == recording.TranscriptionSucceeded && (m.detail.SummaryStatus == recording.TranscriptionFailed || m.detail.SummaryStatus == recording.TranscriptionCancelled) {
				view.WriteString("\nPress m to retry Summary; o to open audio; d to delete; e to edit title; esc returns to history.\n")
			} else if m.detail.TranscriptionStatus == recording.TranscriptionSucceeded {
				view.WriteString("\nPress t to transcribe; m to generate summary; o to open audio; d to delete; e to edit title; esc returns to history.\n")
			} else {
				view.WriteString("\nPress t to transcribe; o to open audio; d to delete; e to edit title; esc returns to history.\n")
			}
		}
		return view.String()
	}
	if m.showSettings {
		view.WriteString("Settings\n\n")
		fmt.Fprintf(&view, "Audio source: %s\n", terminalText(m.settings.AudioSource.Name))
		fmt.Fprintf(&view, "Automatic transcription: %t (a toggles)\n", m.settings.AutomaticTranscription)
		fmt.Fprintf(&view, "Recording limit: %s ([, ], l adjust)\n", m.settings.RecordingLimit)
		fmt.Fprintf(&view, "Whisper executable (w): %s\n", terminalText(m.settings.Processing.WhisperExecutable))
		fmt.Fprintf(&view, "Whisper model (m): %s\n", terminalText(m.settings.Processing.WhisperModel))
		fmt.Fprintf(&view, "CPU threads (t): %d\n", m.settings.Processing.WhisperThreads)
		fmt.Fprintf(&view, "Ollama endpoint (o): %s\n", terminalText(m.settings.Processing.OllamaEndpoint))
		fmt.Fprintf(&view, "Ollama model (n): %s\n", terminalText(m.settings.Processing.OllamaModel))
		if m.settings.ValidationError != "" {
			fmt.Fprintf(&view, "\nConfiguration needs attention: %s\n", terminalText(m.settings.ValidationError))
		}
		if m.err != nil {
			fmt.Fprintf(&view, "\nUnable to save Settings: %v\n", terminalText(m.err.Error()))
		}
		if m.settingsEditing != "" {
			fmt.Fprintf(&view, "\n%s_\n", terminalText(m.settingsInput))
		}
		view.WriteString("\nPress s to save; esc returns. Select Audio source from the main view.\n")
		return view.String()
	}
	view.WriteString("Recording history\n\n")

	if m.err != nil {
		fmt.Fprintf(&view, "Jimpachi error: %v\n", m.err)
		return view.String()
	}
	if len(m.recordings) == 0 {
		view.WriteString("No recordings yet.\n")
		return view.String()
	}

	for index, entry := range m.recordings {
		prefix := "  "
		if m.historyFocused && index == m.historyIndex {
			prefix = "> "
		}
		fmt.Fprintf(&view, "%s%s\n", prefix, terminalText(entry.Title))
		fmt.Fprintf(&view, "%s  %s\n\n", entry.StartedAt.Local().Format("2006-01-02 15:04"), entry.Duration.Round(1e9))
		switch entry.TranscriptionStatus {
		case recording.TranscriptionPending:
			view.WriteString("Post-processing: queued\n\n")
		case recording.TranscriptionRunning:
			view.WriteString("Post-processing: transcribing\n\n")
		case recording.TranscriptionFailed:
			fmt.Fprintf(&view, "Post-processing: failed (%s)\n\n", terminalText(string(entry.TranscriptionFailureCategory)))
		case recording.TranscriptionCancelled:
			view.WriteString("Post-processing: cancelled\n\n")
		}
		switch entry.SummaryStatus {
		case recording.TranscriptionPending:
			view.WriteString("Post-processing: summary queued\n\n")
		case recording.TranscriptionRunning:
			view.WriteString("Post-processing: summarizing\n\n")
		case recording.TranscriptionFailed:
			fmt.Fprintf(&view, "Post-processing: summary failed (%s)\n\n", terminalText(string(entry.SummaryFailureCategory)))
		}
		if entry.AudioMissing {
			view.WriteString("Audio file is missing.\n\n")
		}
		if entry.Interrupted {
			view.WriteString("Interrupted capture recovered after restart.\n\n")
		}
	}
	view.WriteString("Press tab to focus history; up/down and enter open a Recording.\n")

	return view.String()
}

type initialLoaded struct {
	recordings   []recording.Recording
	historyErr   error
	startup      app.Startup
	startupErr   error
	audio        app.AudioState
	audioErr     error
	limit        time.Duration
	limitErr     error
	automatic    bool
	automaticErr error
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
type recordingStarted struct {
	recording app.ActiveRecording
	err       error
	operation uint64
}
type recordingStopped struct {
	recording recording.Recording
	err       error
	operation uint64
}
type recordingTick struct{}
type recordingRenamed struct {
	id    string
	title string
	err   error
}
type recordingLimitSaved struct {
	limit time.Duration
	err   error
}
type transcriptionLoaded struct {
	recording recording.Recording
	err       error
}
type transcriptionRequested struct {
	recording recording.Recording
	err       error
}
type summaryRequested struct {
	recording recording.Recording
	err       error
}
type transcriptionCancelled struct {
	id        string
	recording recording.Recording
	err       error
}
type processingLoaded struct {
	recordings []recording.Recording
	err        error
}
type automaticTranscriptionSaved struct {
	enabled bool
	err     error
}
type recordingOpened struct {
	recording recording.Recording
	err       error
}
type settingsLoaded struct {
	settings app.Settings
	err      error
}
type settingsSaved struct {
	settings app.Settings
	err      error
}
type recordingDeleted struct {
	id  string
	err error
}
type recordingAudioOpened struct{ err error }

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

func (m Model) startRecording(operation uint64) tea.Cmd {
	return func() tea.Msg {
		recording, err := m.workflow.StartRecording(m.ctx)
		return recordingStarted{recording: recording, err: err, operation: operation}
	}
}

func (m Model) stopRecording(operation uint64) tea.Cmd {
	return func() tea.Msg {
		recording, err := m.workflow.StopRecording(m.ctx)
		return recordingStopped{recording: recording, err: err, operation: operation}
	}
}

func (m Model) renameRecording(id, title string) tea.Cmd {
	return func() tea.Msg {
		err := m.workflow.RenameRecording(m.ctx, id, title)
		return recordingRenamed{id: id, title: title, err: err}
	}
}

func (m Model) transcriptionTick() tea.Cmd {
	if m.detail == nil {
		return nil
	}
	id := m.detail.ID
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		recording, err := m.workflow.Recording(m.ctx, id)
		return transcriptionLoaded{recording: recording, err: err}
	})
}

func (m Model) processingTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		recordings, err := m.workflow.History(m.ctx)
		return processingLoaded{recordings: recordings, err: err}
	})
}

func hasActiveProcessing(recordings []recording.Recording) bool {
	for _, entry := range recordings {
		if entry.TranscriptionStatus == recording.TranscriptionPending || entry.TranscriptionStatus == recording.TranscriptionRunning || entry.SummaryStatus == recording.TranscriptionPending || entry.SummaryStatus == recording.TranscriptionRunning {
			return true
		}
	}
	return false
}

func (m Model) requestTranscription(id string) tea.Cmd {
	return func() tea.Msg {
		recording, err := m.workflow.RequestTranscription(m.ctx, id)
		return transcriptionRequested{recording: recording, err: err}
	}
}

func (m Model) requestSummary(id string) tea.Cmd {
	return func() tea.Msg {
		detail, err := m.workflow.RequestSummary(m.ctx, id)
		return summaryRequested{recording: detail, err: err}
	}
}

func (m Model) cancelTranscription(id string) tea.Cmd {
	return func() tea.Msg {
		if err := m.workflow.CancelTranscription(m.ctx, id); err != nil {
			return transcriptionCancelled{id: id, err: err}
		}
		recording, err := m.workflow.Recording(m.ctx, id)
		return transcriptionCancelled{id: id, recording: recording, err: err}
	}
}

func (m Model) cancelSummary(id string) tea.Cmd {
	return func() tea.Msg {
		if err := m.workflow.CancelSummary(m.ctx, id); err != nil {
			return transcriptionCancelled{id: id, err: err}
		}
		detail, err := m.workflow.Recording(m.ctx, id)
		return transcriptionCancelled{id: id, recording: detail, err: err}
	}
}

func (m *Model) replaceHistoryRecording(updated recording.Recording) {
	for index := range m.recordings {
		if m.recordings[index].ID == updated.ID {
			m.recordings[index] = updated
			return
		}
	}
}

func (m Model) openRecording(id string) tea.Cmd {
	return func() tea.Msg {
		recording, err := m.workflow.Recording(m.ctx, id)
		return recordingOpened{recording: recording, err: err}
	}
}

func (m Model) setAutomaticTranscription(enabled bool) tea.Cmd {
	return func() tea.Msg {
		return automaticTranscriptionSaved{enabled: enabled, err: m.workflow.SetAutomaticTranscription(m.ctx, enabled)}
	}
}

func (m Model) loadSettings() tea.Cmd {
	return func() tea.Msg {
		settings, err := m.workflow.Settings(m.ctx)
		return settingsLoaded{settings: settings, err: err}
	}
}

func (m Model) saveSettings(settings app.Settings) tea.Cmd {
	return func() tea.Msg {
		return settingsSaved{settings: settings, err: m.workflow.SaveSettings(m.ctx, settings)}
	}
}

func (m Model) openRecordingAudio(id string) tea.Cmd {
	return func() tea.Msg { return recordingAudioOpened{err: m.workflow.OpenRecordingAudio(m.ctx, id)} }
}

func (m Model) deleteRecording(id string) tea.Cmd {
	return func() tea.Msg { return recordingDeleted{id: id, err: m.workflow.DeleteRecording(m.ctx, id)} }
}

func (m *Model) startSettingsEdit(field string) {
	m.settingsEditing = field
	switch field {
	case "w":
		m.settingsInput = m.settings.Processing.WhisperExecutable
	case "m":
		m.settingsInput = m.settings.Processing.WhisperModel
	case "t":
		m.settingsInput = fmt.Sprint(m.settings.Processing.WhisperThreads)
	case "o":
		m.settingsInput = m.settings.Processing.OllamaEndpoint
	case "n":
		m.settingsInput = m.settings.Processing.OllamaModel
	}
}

func (m *Model) applySettingsInput() {
	switch m.settingsEditing {
	case "w":
		m.settings.Processing.WhisperExecutable = m.settingsInput
	case "m":
		m.settings.Processing.WhisperModel = m.settingsInput
	case "t":
		threads, err := strconv.Atoi(m.settingsInput)
		if err != nil {
			m.err = fmt.Errorf("CPU threads must be a positive integer")
			return
		}
		m.settings.Processing.WhisperThreads = threads
	case "o":
		m.settings.Processing.OllamaEndpoint = m.settingsInput
	case "n":
		m.settings.Processing.OllamaModel = m.settingsInput
	}
}

func (m Model) saveRecordingLimit(limit time.Duration) tea.Cmd {
	return func() tea.Msg {
		return recordingLimitSaved{limit: limit, err: m.workflow.SetRecordingLimit(m.ctx, limit)}
	}
}

func (m *Model) queueRecordingLimit(limit time.Duration) tea.Cmd {
	m.recordingLimit = limit
	if m.limitSaving {
		return nil
	}
	m.limitSaving = true
	m.savingLimit = limit
	return m.saveRecordingLimit(limit)
}

func (m *Model) clearActivity() {
	m.activity = 0
	m.activityErr = nil
}

func meter(activity float64) string {
	// RMS audio levels are typically far below 1.0; perceptual compression keeps
	// quiet but real system output visible without changing capture behavior.
	filled := int(math.Sqrt(activity) * 10)
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

func formatTimestamp(value time.Duration) string {
	return fmt.Sprintf("%02d:%02d:%02d", int(value.Hours()), int(value.Minutes())%60, int(value.Seconds())%60)
}
