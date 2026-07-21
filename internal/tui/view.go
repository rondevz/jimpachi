package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jimpachi/internal/recording"
)

var fieldNotes = struct {
	ink, muted, cyan, amber, green, red, panel, canvas lipgloss.Color
}{
	ink:    lipgloss.Color("#D8DEE9"),
	muted:  lipgloss.Color("#8B97AA"),
	cyan:   lipgloss.Color("#57C7D4"),
	amber:  lipgloss.Color("#E7B75C"),
	green:  lipgloss.Color("#7BC99B"),
	red:    lipgloss.Color("#E06C75"),
	panel:  lipgloss.Color("#242B38"),
	canvas: lipgloss.Color("#11151C"),
}

// View renders Jimpachi as a responsive local Recording notebook.
func (m Model) View() string {
	width, height := m.width, m.height
	if width == 0 {
		width = 72
	}
	if height == 0 {
		height = 32
	}
	if width < 124 {
		return fitTerminal(m.fieldNotesNarrow(width, height), width, height)
	}
	return fitTerminal(m.fieldNotesWide(width, height), width, height)
}

func (m Model) fieldNotesWide(width, height int) string {
	margin := 2
	innerWidth := maxView(1, width-margin*2)
	header := m.fieldHeader(innerWidth)
	bodyHeight := maxView(12, height-5)
	railWidth, contextWidth := 36, 34
	pageWidth := maxView(40, innerWidth-railWidth-contextWidth-2)
	library := m.libraryView(railWidth - 2)
	if m.historyFocused && len(m.recordings) > 0 {
		library = m.focusedLibraryView(railWidth - 2)
	}
	railContent := fieldViewport(library, bodyHeight-2, 0)
	rail := lipgloss.NewStyle().Width(railWidth-2).Height(bodyHeight-2).Padding(1, 1).Render(railContent)
	page := lipgloss.NewStyle().Width(pageWidth-4).Height(bodyHeight-2).Padding(1, 2).Render(m.pageView(pageWidth-4, bodyHeight-2))
	contextContent := fieldViewport(m.contextView(contextWidth-2), bodyHeight-2, 0)
	context := lipgloss.NewStyle().Width(contextWidth-2).Height(bodyHeight-2).Padding(1, 1).Render(contextContent)
	body := lipgloss.JoinHorizontal(lipgloss.Top, rail, fieldDivider(bodyHeight), page, fieldDivider(bodyHeight), context)
	content := header + "\n" + body + "\n" + m.fieldCommandBar(innerWidth)
	return lipgloss.NewStyle().Padding(1, 0, 0, margin).Render(content)
}

func (m Model) fieldNotesNarrow(width, height int) string {
	margin := 1
	innerWidth := maxView(1, width-margin*2)
	header := m.fieldHeader(innerWidth)
	bodyHeight := maxView(8, height-5)
	contentWidth := maxView(1, innerWidth-2)
	var body string
	offset := 0
	if m.detail != nil || m.active != nil || m.showSettings || m.enteringPath {
		body = m.pageView(contentWidth, bodyHeight)
	} else if m.historyFocused && len(m.recordings) > 0 {
		body = m.focusedLibraryView(contentWidth)
	} else {
		body = m.narrowStatusView(contentWidth) + "\n\n" + m.sourcePickerView(contentWidth) + "\n\n" + m.libraryView(contentWidth)
		offset = maxView(0, m.historyIndex*4-4)
		if len(m.recordings) == 0 {
			body += "\n\n" + m.emptyNotebook(contentWidth)
		}
	}
	if notices := m.notificationView(contentWidth); notices != "" && (m.detail != nil || m.active != nil || m.showSettings || m.enteringPath) {
		body = notices + "\n\n" + body
	}
	body = fieldViewport(body, bodyHeight, offset)
	body = lipgloss.NewStyle().Width(contentWidth).Height(bodyHeight).Padding(1, 1).Render(body)
	content := header + "\n" + body + "\n" + m.fieldCommandBar(innerWidth)
	return lipgloss.NewStyle().Padding(1, 0, 0, margin).Render(content)
}

func (m Model) narrowStatusView(width int) string {
	limit := m.recordingLimit.String()
	if m.recordingLimit == 0 {
		limit = "Disabled"
	}
	process := "Manual"
	if m.automatic {
		process = "Automatic"
	}
	view := fieldContextValue("LIMIT", limit) + "  " + fieldContextValue("PROCESS", process)
	if notices := m.notificationView(width); notices != "" {
		view += "\n\n" + notices
	}
	return view
}

func (m Model) notificationView(width int) string {
	var notices []string
	if m.err != nil {
		notices = append(notices, fieldCallout("JIMPACHI ERROR", m.err.Error(), fieldNotes.red, width))
	}
	if m.reminder != "" {
		notices = append(notices, fieldCallout("RESPONSIBILITY", m.reminder, fieldNotes.amber, width))
	}
	if m.discoveryError != "" {
		notices = append(notices, fieldCallout("AUDIO SOURCE", m.discoveryError, fieldNotes.amber, width))
	}
	if m.activityErr != nil {
		notices = append(notices, fieldCallout("AUDIO ACTIVITY", m.activityErr.Error(), fieldNotes.amber, width))
	}
	if m.recoveryWarning != "" {
		notices = append(notices, fieldCallout("RECOVERY", m.recoveryWarning, fieldNotes.amber, width))
	}
	return strings.Join(notices, "\n\n")
}

func (m Model) fieldHeader(width int) string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(fieldNotes.ink).Render("JIMPACHI")
	modeText := "  ●  FIELD NOTES"
	if width < 44 {
		modeText = ""
	}
	mode := lipgloss.NewStyle().Foreground(fieldNotes.muted).Render(modeText)
	status, color := "local-only", fieldNotes.green
	if m.active != nil || m.startPending || m.stopPending {
		status, color = "recording", fieldNotes.red
	} else if hasActiveProcessing(m.recordings) {
		status, color = "processing", fieldNotes.amber
	} else if m.err != nil {
		status, color = "attention", fieldNotes.red
	}
	statusView := lipgloss.NewStyle().Bold(true).Foreground(color).Render("● " + status)
	if width < lipgloss.Width(brand)+lipgloss.Width(statusView)+4 {
		statusView = ""
	}
	gap := maxView(1, width-lipgloss.Width(brand+mode)-lipgloss.Width(statusView)-4)
	return lipgloss.NewStyle().Width(maxView(1, width-2)).Padding(0, minView(1, maxView(0, width-1))).Background(fieldNotes.canvas).Render(brand + mode + strings.Repeat(" ", gap) + statusView)
}

func (m Model) libraryView(width int) string {
	var view strings.Builder
	view.WriteString(fieldSection("LIBRARY", fmt.Sprintf("%d recordings", len(m.recordings))))
	view.WriteString("\n\n")
	if len(m.recordings) == 0 {
		view.WriteString(lipgloss.NewStyle().Foreground(fieldNotes.muted).Render("No recordings yet."))
		return view.String()
	}
	for index, item := range m.recordings {
		view.WriteString(m.recordingEntryView(item, index, width))
	}
	return view.String()
}

func (m Model) focusedLibraryView(width int) string {
	index := minView(m.historyIndex, len(m.recordings)-1)
	view := fieldSection("LIBRARY", fmt.Sprintf("%d of %d · ↑↓ navigate", index+1, len(m.recordings))) + "\n\n"
	view += m.recordingEntryView(m.recordings[index], index, width)
	if index+1 < len(m.recordings) {
		view += lipgloss.NewStyle().Foreground(fieldNotes.muted).Render("NEXT") + "\n\n" + m.recordingEntryView(m.recordings[index+1], index+1, width)
	}
	return view
}

func (m Model) recordingEntryView(item recording.Recording, index, width int) string {
	selected := index == m.historyIndex
	marker := "  "
	titleStyle := lipgloss.NewStyle().Foreground(fieldNotes.muted)
	if selected {
		marker = "○ "
		if m.historyFocused {
			marker = "● "
		}
		titleStyle = titleStyle.Foreground(fieldNotes.cyan).Bold(true)
	}
	view := titleStyle.Render(marker+clipText(item.Title, maxView(8, width-2))) + "\n"
	meta := item.StartedAt.Local().Format("02 Jan 15:04") + "  ·  " + item.Duration.Round(time.Second).String()
	view += lipgloss.NewStyle().Foreground(fieldNotes.muted).Render("  "+meta) + "\n"
	if state := processingState(item); state != "" {
		view += "  " + state + "\n"
	}
	if item.PendingPromotion {
		view += "  " + statusLine("!", "Interrupted capture retained; recovery will retry", fieldNotes.amber) + "\n"
	} else if item.AudioMissing {
		view += "  " + statusLine("!", "Audio file is missing.", fieldNotes.red) + "\n"
	} else if item.Interrupted {
		view += "  " + statusLine("!", "Interrupted capture recovered", fieldNotes.amber) + "\n"
	}
	return view + "\n"
}

func (m Model) pageView(width, height int) string {
	if m.showSettings {
		if m.settingsEditing != "" {
			return m.settingsEditorPage(width)
		}
		return fieldViewport(m.settingsPage(width), height, m.detailScroll)
	}
	if m.enteringPath {
		return fieldViewport(fieldSection("AUDIO SOURCE", "manual monitor")+"\n\n"+lipgloss.NewStyle().Foreground(fieldNotes.cyan).Render("Monitor source path: "+terminalText(m.path)+"_")+"\n\nEnter saves  ·  esc cancels", height, 0)
	}
	if m.active != nil || m.startPending || m.stopPending {
		return fieldViewport(m.recordingPage(width), height, 0)
	}
	if m.detail != nil {
		return fieldViewport(m.detailPage(width), height, m.detailScroll)
	}
	if len(m.recordings) == 0 {
		return m.emptyNotebook(width)
	}
	return m.selectionPreview(width)
}

func (m Model) settingsEditorPage(width int) string {
	labels := map[string]string{"w": "Whisper executable", "m": "Whisper model", "t": "CPU threads", "o": "Ollama endpoint", "n": "Ollama model"}
	label := labels[m.settingsEditing]
	return fieldSection("EDIT SETTING", label) + "\n\n" + lipgloss.NewStyle().Foreground(fieldNotes.cyan).Render(wrapText(terminalText(m.settingsInput)+"_", width)) + "\n\n" + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render("enter applies  ·  esc cancels")
}

func (m Model) emptyNotebook(width int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(fieldNotes.ink).Render("Preserve your first instruction")
	copy := wrapText("Select a system-output monitor, then start a Recording. Audio stays local and remains the source of truth.", width)
	action := lipgloss.NewStyle().Bold(true).Foreground(fieldNotes.canvas).Background(fieldNotes.red).Padding(0, 2).Render("r  START RECORDING")
	return fieldSection("NEW NOTE", "system audio") + "\n\n" + title + "\n\n" + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render(copy) + "\n\n" + action
}

func (m Model) selectionPreview(width int) string {
	item := m.recordings[minView(m.historyIndex, len(m.recordings)-1)]
	return fieldSection("RECORDING NOTE", "enter to open") + "\n\n" + lipgloss.NewStyle().Bold(true).Foreground(fieldNotes.ink).Render(terminalText(item.Title)) + "\n" + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render(item.StartedAt.Local().Format("02 JAN 2006  ·  15:04")+"  ·  "+item.Duration.Round(time.Second).String()) + "\n\n" + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render(wrapText("Open this Recording to review its Summary, full timestamped Transcription, and authoritative audio artifact.", width))
}

func (m Model) recordingPage(width int) string {
	state := "STARTING"
	color := fieldNotes.amber
	duration := "0s"
	source := m.selected.Name
	if m.active != nil {
		state, color = "RECORDING", fieldNotes.red
		duration = time.Since(m.active.StartedAt).Round(time.Second).String()
		source = m.active.Source.Name
	}
	if m.stopPending {
		state, color = "FINALIZING", fieldNotes.amber
	}
	badge := lipgloss.NewStyle().Bold(true).Foreground(color).Render("● " + state)
	timer := lipgloss.NewStyle().Bold(true).Foreground(fieldNotes.ink).Render(duration)
	view := fieldSection("ACTIVE NOTE", "system output") + "\n\n" + badge + "\n\n" + timer + "\n" + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render(terminalText(source))
	if m.warning != "" {
		view += "\n\n" + lipgloss.NewStyle().Foreground(fieldNotes.amber).Render(wrapText(m.warning, width))
	}
	view += "\n\n" + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render("Press s to stop and preserve this Recording.")
	return view
}

func (m Model) detailPage(width int) string {
	detail := m.detail
	title := lipgloss.NewStyle().Bold(true).Foreground(fieldNotes.ink).Render(terminalText(detail.Title))
	meta := detail.StartedAt.Local().Format("02 JAN 2006  ·  15:04") + "  ·  " + detail.Duration.Round(time.Second).String()
	view := fieldSection("RECORDING DETAIL", clipText(detail.ID, 16)) + "\n\n" + title + "\n" + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render(meta) + "\n\n"
	if m.editingTitle {
		return view + fieldSection("EDIT TITLE", "enter saves · esc cancels") + "\n\n" + lipgloss.NewStyle().Foreground(fieldNotes.cyan).Render(terminalText(m.title)+"_")
	}
	if m.confirmDeletion {
		return view + fieldCallout("DELETE RECORDING", "This permanently removes the audio, Transcription, Summary, and queued work. Press y to confirm or n to cancel.", fieldNotes.red, width)
	}
	if detail.PendingPromotion {
		view += fieldCallout("RECOVERY PENDING", "Interrupted capture retained; recovery will be retried on the next start.", fieldNotes.amber, width) + "\n\n"
	} else if detail.AudioMissing {
		view += fieldCallout("AUDIO UNAVAILABLE", "The local audio file is missing.", fieldNotes.red, width) + "\n\n"
	} else if detail.Interrupted {
		view += fieldCallout("RECOVERED", "Interrupted capture recovered after restart.", fieldNotes.amber, width) + "\n\n"
	}
	tab := m.effectiveDetailTab()
	view += fieldTabs(tab, "Summary", "Transcript", "Recording") + "\n\n"
	switch tab {
	case 1:
		view += m.transcriptPage(width)
	case 2:
		view += m.artifactPage(width)
	default:
		view += m.summaryPage(width)
	}
	return view
}

func (m Model) effectiveDetailTab() int {
	if m.detailTabSet || m.detail == nil {
		return m.detailTab
	}
	return initialDetailTab(*m.detail)
}

func (m *Model) initializeDetailTab(detail recording.Recording) {
	m.detailTab = initialDetailTab(detail)
	m.detailTabSet = true
	m.detailScroll = 0
}

func initialDetailTab(detail recording.Recording) int {
	if detail.SummaryStatus != "" && detail.SummaryStatus != recording.TranscriptionNotQueued {
		return 0
	}
	if (detail.TranscriptionStatus != "" && detail.TranscriptionStatus != recording.TranscriptionNotQueued) || len(detail.Transcription) > 0 {
		return 1
	}
	return 2
}

func (m Model) summaryPage(width int) string {
	detail := m.detail
	view := fieldSection("SUMMARY", "auxiliary interpretation") + "\n\n"
	switch detail.SummaryStatus {
	case recording.TranscriptionPending:
		return view + statusLine("◌", "Summary queued", fieldNotes.amber)
	case recording.TranscriptionRunning:
		message := "Generating Summary"
		if detail.SummaryProgress > 0 {
			message = fmt.Sprintf("Generating Summary · %d characters", detail.SummaryProgress)
		}
		return view + statusLine("◌", message, fieldNotes.amber)
	case recording.TranscriptionFailed:
		return view + fieldCallout("SUMMARY FAILED", detail.SummaryError, fieldNotes.red, width) + "\n\n" + fieldAction("m", "Retry Summary", fieldNotes.cyan)
	case recording.TranscriptionCancelled:
		return view + statusLine("×", "Summary cancelled", fieldNotes.muted)
	case recording.TranscriptionSucceeded:
		if detail.Summary.Overview != "" {
			view += lipgloss.NewStyle().Foreground(fieldNotes.ink).Render(wrapText(terminalText(detail.Summary.Overview), width)) + "\n\n"
		}
		sections := []struct {
			name   string
			values []string
			color  lipgloss.Color
		}{{"AGREEMENTS & DECISIONS", detail.Summary.Agreements, fieldNotes.green}, {"SUGGESTIONS", detail.Summary.Suggestions, fieldNotes.amber}, {"ACTION ITEMS", detail.Summary.ActionItems, fieldNotes.cyan}, {"DEADLINES", detail.Summary.Deadlines, fieldNotes.amber}, {"OPEN QUESTIONS", detail.Summary.OpenQuestions, fieldNotes.cyan}}
		for _, section := range sections {
			if len(section.values) == 0 {
				continue
			}
			view += fieldCallout(section.name, strings.Join(section.values, "\n• "), section.color, width) + "\n\n"
		}
		view += lipgloss.NewStyle().Italic(true).Foreground(fieldNotes.muted).Render(wrapText("The Recording is authoritative. Verify this interpretation against the Transcription and audio.", width))
		return view
	default:
		return view + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render("No Summary yet. Press m to generate one.")
	}
}

func (m Model) transcriptPage(width int) string {
	detail := m.detail
	view := fieldSection("TRANSCRIPTION", "derived from local audio") + "\n\n"
	switch detail.TranscriptionStatus {
	case recording.TranscriptionPending:
		return view + statusLine("◌", "Transcription queued", fieldNotes.amber)
	case recording.TranscriptionRunning:
		return view + statusLine("◌", "Transcribing locally", fieldNotes.amber)
	case recording.TranscriptionFailed:
		return view + fieldCallout("TRANSCRIPTION FAILED", detail.TranscriptionError+" Press t to retry.", fieldNotes.red, width)
	case recording.TranscriptionCancelled:
		return view + statusLine("×", "Transcription cancelled", fieldNotes.muted)
	}
	if len(detail.Transcription) == 0 {
		return view + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render("No speech was detected. Press t to transcribe again.")
	}
	for _, segment := range detail.Transcription {
		stamp := lipgloss.NewStyle().Foreground(fieldNotes.cyan).Render("[" + formatTimestamp(segment.Start) + " - " + formatTimestamp(segment.End) + "]")
		if width <= lipgloss.Width(stamp)+2 {
			start := lipgloss.NewStyle().Foreground(fieldNotes.cyan).Render("[" + formatTimestamp(segment.Start) + " -")
			end := lipgloss.NewStyle().Foreground(fieldNotes.cyan).Render(" " + formatTimestamp(segment.End) + "]")
			view += start + "\n" + end + "\n" + lipgloss.NewStyle().Foreground(fieldNotes.ink).Render(wrapText(terminalText(segment.Text), width)) + "\n\n"
			continue
		}
		indent := strings.Repeat(" ", lipgloss.Width(stamp)+2)
		textWidth := maxView(1, width-lipgloss.Width(stamp)-2)
		lines := strings.Split(wrapText(terminalText(segment.Text), textWidth), "\n")
		for index, line := range lines {
			if index == 0 {
				view += stamp + "  "
			} else {
				view += indent
			}
			view += lipgloss.NewStyle().Foreground(fieldNotes.ink).Render(line) + "\n"
		}
		view += "\n"
	}
	return strings.TrimSpace(view)
}

func (m Model) artifactPage(width int) string {
	detail := m.detail
	view := fieldSection("RECORDING", "authoritative artifact") + "\n\n"
	view += fieldLabelValue("ID", detail.ID) + "\n"
	view += fieldLabelValue("STARTED", detail.StartedAt.Local().Format("2006-01-02 15:04:05")) + "\n"
	view += fieldLabelValue("DURATION", detail.Duration.Round(time.Second).String()) + "\n"
	view += fieldLabelValue("AUDIO", terminalText(detail.AudioPath)) + "\n\n"
	view += lipgloss.NewStyle().Foreground(fieldNotes.cyan).Bold(true).Render("o  Open audio")
	return wrapLongLines(view, width)
}

func (m Model) settingsPage(width int) string {
	view := fieldSection("SETTINGS", "local configuration") + "\n\n"
	view += fieldLabelValue("AUDIO SOURCE", terminalText(m.settings.AudioSource.Name)) + "\n"
	view += fieldLabelValue("AUTO TRANSCRIBE", fmt.Sprintf("%t  ·  a toggles", m.settings.AutomaticTranscription)) + "\n"
	view += fieldLabelValue("RECORDING LIMIT", m.settings.RecordingLimit.String()+"  ·  [ ] adjust · l toggles") + "\n\n"
	view += fieldSection("LOCAL TRANSCRIPTION", "") + "\n\n"
	view += fieldLabelValue("w  EXECUTABLE", terminalText(m.settings.Processing.WhisperExecutable)) + "\n"
	view += fieldLabelValue("m  MODEL", terminalText(m.settings.Processing.WhisperModel)) + "\n"
	view += fieldLabelValue("t  CPU THREADS", fmt.Sprint(m.settings.Processing.WhisperThreads)) + "\n\n"
	view += fieldSection("LOCAL SUMMARY", "") + "\n\n"
	view += fieldLabelValue("o  ENDPOINT", terminalText(m.settings.Processing.OllamaEndpoint)) + "\n"
	view += fieldLabelValue("n  MODEL", terminalText(m.settings.Processing.OllamaModel)) + "\n"
	if m.settings.ValidationError != "" {
		view += "\n" + fieldCallout("CONFIGURATION NEEDS ATTENTION", m.settings.ValidationError, fieldNotes.amber, width)
	}
	if m.err != nil {
		view += "\n" + fieldCallout("UNABLE TO SAVE SETTINGS", m.err.Error(), fieldNotes.red, width)
	}
	if m.settingsEditing != "" {
		view += "\n\n" + lipgloss.NewStyle().Foreground(fieldNotes.cyan).Render(terminalText(m.settingsInput)+"_")
	}
	view += "\n\n" + lipgloss.NewStyle().Foreground(fieldNotes.muted).Render("s saves  ·  esc returns")
	return wrapLongLines(view, width)
}

func (m Model) contextView(width int) string {
	var view strings.Builder
	view.WriteString(fieldSection("CONTEXT", ""))
	view.WriteString("\n\n")
	if notices := m.notificationView(width); notices != "" {
		view.WriteString(notices)
		view.WriteString("\n\n")
	}
	source := m.selected.Name
	if source == "" {
		source = "Not selected"
	}
	view.WriteString(fieldContextValue("SOURCE", clipText(source, 18)) + "\n")
	limit := m.recordingLimit.String()
	if m.recordingLimit == 0 {
		limit = "Disabled"
	}
	view.WriteString(fieldContextValue("LIMIT", limit) + "\n")
	automatic := "Manual"
	if m.automatic {
		automatic = "Automatic"
	}
	view.WriteString(fieldContextValue("PROCESS", automatic) + "\n\n")
	view.WriteString(m.sourcePickerView(width))
	view.WriteString("\n\n")
	if m.detail != nil {
		view.WriteString(fieldSection("ARTIFACTS", "") + "\n\n")
		view.WriteString(artifactStatus("Audio", !m.detail.AudioMissing) + "\n")
		view.WriteString(artifactStatus("Transcript", m.detail.TranscriptionStatus == recording.TranscriptionSucceeded) + "\n")
		view.WriteString(artifactStatus("Summary", m.detail.SummaryStatus == recording.TranscriptionSucceeded) + "\n\n")
		view.WriteString(fieldSection("ACTIONS", "") + "\n\n")
		view.WriteString(fieldAction("o", "Open audio", fieldNotes.cyan) + "\n")
		view.WriteString(fieldAction("e", "Rename", fieldNotes.cyan) + "\n")
		if m.detail.TranscriptionStatus == recording.TranscriptionPending || m.detail.TranscriptionStatus == recording.TranscriptionRunning || m.detail.SummaryStatus == recording.TranscriptionPending || m.detail.SummaryStatus == recording.TranscriptionRunning {
			view.WriteString(fieldAction("c", "Cancel processing", fieldNotes.amber) + "\n")
		} else {
			view.WriteString(fieldAction("t", "Transcribe", fieldNotes.cyan) + "\n")
			if m.detail.TranscriptionStatus == recording.TranscriptionSucceeded {
				view.WriteString(fieldAction("m", "Summarize", fieldNotes.cyan) + "\n")
			}
		}
		view.WriteString(fieldAction("d", "Delete", fieldNotes.red))
	} else {
		view.WriteString(fieldSection("CAPTURE", "") + "\n\n")
		view.WriteString(fieldAction("r", "New Recording", fieldNotes.red) + "\n")
		view.WriteString(fieldAction("g", "Settings", fieldNotes.cyan))
	}
	return view.String()
}

func (m Model) sourcePickerView(width int) string {
	var view strings.Builder
	meta := "↑↓ · enter selects"
	if len(m.sources) > 5 {
		meta = fmt.Sprintf("%d sources · ↑↓", len(m.sources))
	}
	view.WriteString(fieldSection("AUDIO SOURCE", meta))
	view.WriteString("\n\n")
	if len(m.sources) == 0 {
		view.WriteString(lipgloss.NewStyle().Foreground(fieldNotes.muted).Render("No system-output monitors found."))
		if m.canUsePath {
			view.WriteString("\n" + fieldAction("a", "Enter monitor path", fieldNotes.cyan))
		}
		return view.String()
	}
	start := maxView(0, m.sourceIndex-2)
	end := minView(len(m.sources), start+5)
	if end-start < 5 {
		start = maxView(0, end-5)
	}
	for index := start; index < end; index++ {
		source := m.sources[index]
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(fieldNotes.muted)
		if index == m.sourceIndex {
			prefix = "· "
			if !m.historyFocused {
				prefix = "> "
			}
			style = style.Foreground(fieldNotes.cyan).Bold(true)
		}
		nameWidth := maxView(8, width-4)
		if index == m.sourceIndex && !source.Pulse {
			nameWidth = maxView(8, width-17)
		}
		line := prefix + clipText(source.Name, nameWidth)
		if index == m.sourceIndex {
			line += sourceActivityView(source, m.activity)
		}
		view.WriteString(style.Render(line))
		if index+1 < end {
			view.WriteByte('\n')
		}
	}
	return view.String()
}

func (m Model) fieldCommandBar(width int) string {
	commands := []string{"q quit"}
	if m.showSettings {
		commands = append([]string{"s save", "esc close"}, commands...)
	} else if m.detail != nil {
		commands = append([]string{"tab sections", "↑↓ scroll", "o audio", "e rename", "d delete", "esc library"}, commands...)
	} else if m.active != nil || m.startPending || m.stopPending {
		commands = append([]string{"s stop"}, commands...)
	} else {
		focus := "source"
		if m.historyFocused {
			focus = "library"
		}
		commands = append([]string{"r record", "tab focus", "↑↓ " + focus, "enter select", "g settings"}, commands...)
	}
	var parts []string
	for _, command := range commands {
		pieces := strings.SplitN(command, " ", 2)
		key := lipgloss.NewStyle().Foreground(fieldNotes.cyan).Bold(true).Render(pieces[0])
		label := ""
		if len(pieces) > 1 && width >= 48 {
			label = lipgloss.NewStyle().Foreground(fieldNotes.muted).Render(" " + pieces[1])
		}
		parts = append(parts, key+label)
	}
	separator := "    "
	if width < 48 {
		separator = "  "
	}
	content := strings.Join(parts, separator)
	for len(parts) > 1 && lipgloss.Width(content) > maxView(1, width-2) {
		parts = append(parts[:len(parts)-2], parts[len(parts)-1])
		content = strings.Join(parts, separator)
	}
	return lipgloss.NewStyle().Width(maxView(1, width-2)).Padding(0, minView(1, maxView(0, width-1))).Background(fieldNotes.canvas).Render(content)
}

func fieldSection(title, meta string) string {
	view := lipgloss.NewStyle().Bold(true).Foreground(fieldNotes.muted).Render(title)
	if meta != "" {
		view += "  " + lipgloss.NewStyle().Foreground(fieldNotes.muted).Faint(true).Render(meta)
	}
	return view
}

func fieldTabs(active int, labels ...string) string {
	var tabs []string
	for index, label := range labels {
		style := lipgloss.NewStyle().Foreground(fieldNotes.muted).Padding(0, 1)
		if index == active {
			style = style.Foreground(fieldNotes.cyan).Bold(true).BorderBottom(true).BorderForeground(fieldNotes.cyan)
		}
		tabs = append(tabs, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)
}

func fieldCallout(title, text string, color lipgloss.Color, width int) string {
	content := lipgloss.NewStyle().Foreground(color).Bold(true).Render(title) + "\n" + lipgloss.NewStyle().Foreground(fieldNotes.ink).Render(wrapText(terminalText(text), maxView(8, width-4)))
	return lipgloss.NewStyle().BorderLeft(true).BorderForeground(color).PaddingLeft(2).Width(maxView(8, width-4)).Render(content)
}

func fieldLabelValue(label, value string) string {
	return lipgloss.NewStyle().Foreground(fieldNotes.muted).Width(16).Render(label) + lipgloss.NewStyle().Foreground(fieldNotes.ink).Render(value)
}

func fieldContextValue(label, value string) string {
	return lipgloss.NewStyle().Foreground(fieldNotes.muted).Width(9).Render(label) + lipgloss.NewStyle().Foreground(fieldNotes.ink).Render(value)
}

func fieldAction(key, label string, color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Bold(true).Width(3).Render(key) + lipgloss.NewStyle().Foreground(fieldNotes.ink).Render(label)
}

func statusLine(mark, text string, color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(mark + "  " + text)
}

func artifactStatus(label string, available bool) string {
	mark, color := "○", fieldNotes.muted
	if available {
		mark, color = "●", fieldNotes.green
	}
	return lipgloss.NewStyle().Foreground(color).Render(mark+"  ") + lipgloss.NewStyle().Foreground(fieldNotes.ink).Render(label)
}

func processingState(item recording.Recording) string {
	if item.TranscriptionStatus == recording.TranscriptionRunning {
		return statusLine("◌", "Transcribing", fieldNotes.amber)
	}
	if item.SummaryStatus == recording.TranscriptionRunning {
		return statusLine("◌", "Summarizing", fieldNotes.amber)
	}
	if item.TranscriptionStatus == recording.TranscriptionPending || item.SummaryStatus == recording.TranscriptionPending {
		return statusLine("◌", "Post-processing: queued", fieldNotes.amber)
	}
	if item.TranscriptionStatus == recording.TranscriptionFailed || item.SummaryStatus == recording.TranscriptionFailed {
		return statusLine("!", "Needs attention", fieldNotes.red)
	}
	return ""
}

func fieldDivider(height int) string {
	if height < 2 {
		return lipgloss.NewStyle().Foreground(fieldNotes.panel).Render("│")
	}
	return lipgloss.NewStyle().Foreground(fieldNotes.panel).Render(strings.Repeat("│\n", height-1) + "│")
}

func fieldViewport(text string, height, offset int) string {
	lines := strings.Split(text, "\n")
	maxOffset := maxView(0, len(lines)-height)
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	end := minView(len(lines), offset+height)
	return strings.Join(lines[offset:end], "\n")
}

func (m Model) maxContentScroll() int {
	if m.settingsEditing != "" {
		return 0
	}
	width, height := m.width, m.height
	if width == 0 {
		width = 72
	}
	if height == 0 {
		height = 32
	}
	contentWidth := maxView(1, width-4)
	viewportHeight := maxView(1, height-5)
	if width >= 124 {
		contentWidth = maxView(1, (width-4)-36-34-2-4)
		viewportHeight = maxView(1, height-7)
	}
	content := ""
	if m.showSettings {
		content = m.settingsPage(contentWidth)
	} else if m.detail != nil {
		content = m.detailPage(contentWidth)
	}
	return maxView(0, len(strings.Split(content, "\n"))-viewportHeight)
}

func wrapLongLines(text string, width int) string {
	lines := strings.Split(text, "\n")
	for index := range lines {
		lines[index] = wrapText(lines[index], width)
	}
	return strings.Join(lines, "\n")
}

func clipText(text string, width int) string {
	runes := []rune(terminalText(text))
	if len(runes) <= width {
		return string(runes)
	}
	if width <= 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func maxView(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minView(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func fitTerminal(view string, width, height int) string {
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = append(lines[:maxView(0, height-1)], lines[len(lines)-1])
	}
	for index := range lines {
		lines[index] = ansi.TruncateWc(lines[index], width, "")
	}
	return strings.Join(lines, "\n")
}
