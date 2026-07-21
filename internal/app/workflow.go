package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"jimpachi/internal/audio"
	"jimpachi/internal/recording"
	"jimpachi/internal/transcription"
)

// Workflow coordinates Jimpachi behavior for the terminal UI.
type Workflow struct {
	history     *recording.SQLite
	audio       audio.Adapter
	transcriber Transcriber
	scheduler   Scheduler

	audioSourceMu            sync.Mutex
	latestAudioSourceVersion uint64
	recordingMu              sync.Mutex
	activeRecording          *activeRecording
	captureFailure           error
	captureCompleted         *recording.Recording
	recoveryWarning          string
	limitStopTimeout         time.Duration
	processingMu             sync.Mutex
	workerCancel             context.CancelFunc
	workerWake               chan struct{}
	workerWG                 sync.WaitGroup
	runningCancel            context.CancelFunc
	runningDone              chan struct{}
	runningID                string
	runningAttempt           uint64
	runningCancelledByUser   bool
	processingPaused         bool
}

// Transcriber is the external local speech-to-text boundary used by Workflow.
type Transcriber interface {
	Transcribe(context.Context, string) ([]transcription.Segment, error)
}

type activeRecording struct {
	recording       recording.Recording
	source          audio.Source
	capture         audio.Capture
	temporary       string
	stopping        bool
	stopped         bool
	promoted        bool
	limitReached    bool
	warned          bool
	warning         Timer
	warningDuration time.Duration
	limit           Timer
}

// CaptureState reports the active Recording and its latest terminal failure.
type CaptureState struct {
	Active    *ActiveRecording
	Failure   string
	Warning   string
	Completed *recording.Recording
}

// Scheduler supplies workflow time and can be faked to test Recording limits.
type Scheduler interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) Timer
}

// Timer cancels a scheduled workflow action.
type Timer interface{ Stop() bool }

type realScheduler struct{}

func (realScheduler) Now() time.Time { return time.Now() }
func (realScheduler) AfterFunc(after time.Duration, action func()) Timer {
	return time.AfterFunc(after, action)
}

const recordingResponsibilityReminder = "Record system output only when everyone involved knows and agrees. You are responsible for complying with applicable laws and policies."

var errStaleAudioSourceSelection = errors.New("stale Audio source selection")

// Open creates a workflow backed by local Recording history.
func Open(ctx context.Context, dataDir string) (*Workflow, error) {
	return OpenWithAudio(ctx, dataDir, audio.New())
}

// OpenWithAudio creates a workflow using an operating-system audio adapter.
func OpenWithAudio(ctx context.Context, dataDir string, adapter audio.Adapter) (*Workflow, error) {
	whisper, err := transcription.LoadConfiguredWhisper()
	if err != nil {
		return nil, err
	}
	return OpenWithAudioAndTranscriber(ctx, dataDir, adapter, whisper)
}

// OpenWithAudioAndTranscriber creates a workflow with replaceable local transcription.
func OpenWithAudioAndTranscriber(ctx context.Context, dataDir string, adapter audio.Adapter, transcriber Transcriber) (*Workflow, error) {
	return open(ctx, dataDir, adapter, transcriber, realScheduler{})
}

// OpenWithAudioAndScheduler creates a workflow with a controllable time seam.
func OpenWithAudioAndScheduler(ctx context.Context, dataDir string, adapter audio.Adapter, scheduler Scheduler) (*Workflow, error) {
	return open(ctx, dataDir, adapter, transcription.Whisper{}, scheduler)
}

func open(ctx context.Context, dataDir string, adapter audio.Adapter, transcriber Transcriber, scheduler Scheduler) (*Workflow, error) {
	history, err := recording.OpenSQLite(ctx, dataDir)
	if err != nil {
		return nil, fmt.Errorf("open application workflow: %w", err)
	}

	if err := history.ReconcileAudio(ctx, func(path string) (bool, error) { return adapter.Playable(ctx, path) }); err != nil {
		_ = history.Close()
		return nil, fmt.Errorf("recover interrupted Recordings: %w", err)
	}
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workflow := &Workflow{history: history, audio: adapter, transcriber: transcriber, scheduler: scheduler, recoveryWarning: history.RecoveryWarning(), limitStopTimeout: 5 * time.Second, workerCancel: workerCancel, workerWake: make(chan struct{}, 1)}
	workflow.workerWG.Add(1)
	go workflow.runTranscriptionWorker(workerCtx)
	if err := history.ResetRunningTranscriptions(ctx); err != nil {
		_ = workflow.Close()
		return nil, fmt.Errorf("recover running Transcriptions: %w", err)
	}
	if enabled, err := workflow.AutomaticTranscription(ctx); err != nil {
		_ = workflow.Close()
		return nil, err
	} else if enabled {
		workflow.wakeTranscriptionWorker()
	}
	return workflow, nil
}

// DataDir returns the XDG data directory for Jimpachi.
func DataDir() (string, error) {
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		if !filepath.IsAbs(dataHome) {
			return "", fmt.Errorf("XDG_DATA_HOME must be an absolute path")
		}

		return filepath.Join(dataHome, "jimpachi"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home directory: %w", err)
	}

	return filepath.Join(home, ".local", "share", "jimpachi"), nil
}

// Startup returns initial user-visible application state.
func (w *Workflow) Startup(ctx context.Context) (Startup, error) {
	startup := Startup{}
	startup.RecoveryWarning = w.recoveryWarning
	seen, ok, err := w.history.Setting(ctx, "recording_responsibility_reminder_seen")
	if err != nil {
		return Startup{}, fmt.Errorf("load recording-responsibility reminder state: %w", err)
	}
	if !ok || seen != "true" {
		startup.Reminder = recordingResponsibilityReminder
		if err := w.history.SaveSetting(ctx, "recording_responsibility_reminder_seen", "true"); err != nil {
			return Startup{}, fmt.Errorf("save recording-responsibility reminder state: %w", err)
		}
	}

	return startup, nil
}

// Startup is the initial user-visible state of a Workflow.
type Startup struct {
	Reminder        string
	RecoveryWarning string
}

// AudioState contains source choices and any discovery guidance for the UI.
type AudioState struct {
	Sources            []audio.Source
	Selected           audio.Source
	DiscoveryError     string
	CanUseExplicitPath bool
}

// AudioSources discovers system-output sources and restores the selected one.
func (w *Workflow) AudioSources(ctx context.Context) (AudioState, error) {
	sources, discoveryErr := w.audio.Sources(ctx)
	state := AudioState{Sources: sources}
	if discoveryErr != nil {
		if len(sources) == 0 {
			state.DiscoveryError = fmt.Sprintf("Could not discover system-output sources: %v. Press a to enter a monitor source path.", discoveryErr)
			state.CanUseExplicitPath = true
		} else {
			state.DiscoveryError = discoveryErr.Error()
		}
	}

	selectedID, ok, err := w.history.Setting(ctx, "audio_source_id")
	if err != nil {
		return AudioState{}, fmt.Errorf("load selected Audio source: %w", err)
	}
	if ok {
		selectedName, _, err := w.history.Setting(ctx, "audio_source_name")
		if err != nil {
			return AudioState{}, fmt.Errorf("load selected Audio source name: %w", err)
		}
		explicit, _, err := w.history.Setting(ctx, "audio_source_explicit")
		if err != nil {
			return AudioState{}, fmt.Errorf("load selected Audio source type: %w", err)
		}
		state.Selected = audio.Source{ID: selectedID, Name: selectedName, Explicit: explicit == "true"}
		found := false
		for _, source := range state.Sources {
			if source.ID == selectedID {
				found = true
				break
			}
		}
		if !found {
			state.Sources = append([]audio.Source{state.Selected}, state.Sources...)
		}
		return state, nil
	}
	if len(sources) > 0 {
		state.Selected = sources[0]
	}

	return state, nil
}

// SelectAudioSource persists the newest Audio source selected by the user.
func (w *Workflow) SelectAudioSource(ctx context.Context, source audio.Source, version uint64) error {
	if source.ID == "" {
		return fmt.Errorf("select Audio source: source path is required")
	}
	if source.Name == "" {
		source.Name = source.ID
	}
	w.audioSourceMu.Lock()
	defer w.audioSourceMu.Unlock()
	if version <= w.latestAudioSourceVersion {
		return errStaleAudioSourceSelection
	}
	w.latestAudioSourceVersion = version
	if err := w.history.SaveSettings(ctx, map[string]string{
		"audio_source_id":       source.ID,
		"audio_source_name":     source.Name,
		"audio_source_explicit": fmt.Sprintf("%t", source.Explicit),
	}); err != nil {
		return fmt.Errorf("save selected Audio source: %w", err)
	}

	return nil
}

// AudioActivity returns the current normalized activity for an Audio source.
func (w *Workflow) AudioActivity(ctx context.Context, source audio.Source) (float64, error) {
	activity, err := w.audio.Activity(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("measure Audio source activity: %w", err)
	}
	if activity < 0 {
		return 0, nil
	}
	if activity > 1 {
		return 1, nil
	}

	return activity, nil
}

// ActiveRecording is the user-visible state of an in-progress Recording.
type ActiveRecording struct {
	ID        string
	StartedAt time.Time
	Source    audio.Source
}

const defaultRecordingLimit = time.Hour
const recordingLimitWarning = 5 * time.Minute

// RecordingLimit returns the configured maximum duration, or zero when disabled.
func (w *Workflow) RecordingLimit(ctx context.Context) (time.Duration, error) {
	value, found, err := w.history.Setting(ctx, "recording_limit_minutes")
	if err != nil {
		return 0, fmt.Errorf("load Recording limit: %w", err)
	}
	if !found {
		return defaultRecordingLimit, nil
	}
	minutes, err := strconv.Atoi(value)
	if err != nil || minutes < 0 {
		return 0, fmt.Errorf("load Recording limit: invalid value %q", value)
	}
	return time.Duration(minutes) * time.Minute, nil
}

// SetRecordingLimit persists a maximum Recording duration; zero disables it.
func (w *Workflow) SetRecordingLimit(ctx context.Context, limit time.Duration) error {
	if limit < 0 || limit%time.Minute != 0 {
		return fmt.Errorf("set Recording limit: use a whole number of minutes or zero")
	}
	if err := w.history.SaveSetting(ctx, "recording_limit_minutes", strconv.FormatInt(int64(limit/time.Minute), 10)); err != nil {
		return fmt.Errorf("save Recording limit: %w", err)
	}
	return nil
}

// StartRecording begins capture from the persisted Audio source.
func (w *Workflow) StartRecording(ctx context.Context) (ActiveRecording, error) {
	w.pauseTranscription()
	captureStarted := false
	defer func() {
		if captureStarted {
			return
		}
		w.recordingMu.Lock()
		active := w.activeRecording != nil
		w.recordingMu.Unlock()
		if !active {
			w.resumeTranscription()
		}
	}()
	w.cancelAndAwaitTranscription()
	w.recordingMu.Lock()
	defer w.recordingMu.Unlock()
	if w.activeRecording != nil {
		return ActiveRecording{}, fmt.Errorf("start Recording: a Recording is already active")
	}
	state, err := w.AudioSources(ctx)
	if err != nil {
		return ActiveRecording{}, fmt.Errorf("start Recording: load selected Audio source: %w", err)
	}
	if state.Selected.ID == "" {
		return ActiveRecording{}, fmt.Errorf("start Recording: select an Audio source first")
	}
	limit, err := w.RecordingLimit(ctx)
	if err != nil {
		return ActiveRecording{}, err
	}
	startedAt := w.scheduler.Now().UTC()
	id, err := recordingID()
	if err != nil {
		return ActiveRecording{}, fmt.Errorf("start Recording: %w", err)
	}
	recording := recording.Recording{
		ID:        id,
		Title:     "Recording " + startedAt.Local().Format("2006-01-02 15:04"),
		StartedAt: startedAt,
		AudioPath: filepath.Join(w.history.DataDir(), "recordings", id+".opus"),
	}
	// Keep the .opus suffix on staged output so FFmpeg can infer its container.
	temporary := strings.TrimSuffix(recording.AudioPath, ".opus") + ".partial.opus"
	recording.PendingPromotion = true
	recording.Interrupted = true
	if err := w.history.Save(ctx, recording); err != nil {
		return ActiveRecording{}, fmt.Errorf("save pending Recording: %w", err)
	}
	capture, err := w.audio.Start(ctx, state.Selected, temporary)
	if err != nil {
		_ = w.history.Delete(ctx, recording.ID)
		_ = os.Remove(temporary)
		return ActiveRecording{}, fmt.Errorf("start Recording capture: %w", err)
	}
	active := &activeRecording{recording: recording, source: state.Selected, capture: capture, temporary: temporary}
	w.captureFailure = nil
	w.captureCompleted = nil
	w.activeRecording = active
	if limit > 0 {
		active.warningDuration = recordingLimitWarning
		if limit <= recordingLimitWarning {
			active.warningDuration = limit / 2
		}
		active.warning = w.scheduler.AfterFunc(limit-active.warningDuration, func() { w.warnRecording(active) })
		active.limit = w.scheduler.AfterFunc(limit, func() { w.stopAtLimit(active) })
	}
	go w.watchCapture(active)
	captureStarted = true
	return ActiveRecording{ID: id, StartedAt: startedAt, Source: state.Selected}, nil
}

// StopRecording stops capture, saves the completed Recording, and returns it.
func (w *Workflow) StopRecording(ctx context.Context) (recording.Recording, error) {
	w.recordingMu.Lock()
	if w.activeRecording == nil {
		w.recordingMu.Unlock()
		return recording.Recording{}, fmt.Errorf("stop Recording: no Recording is active")
	}
	active := w.activeRecording
	if active.stopping {
		w.recordingMu.Unlock()
		return recording.Recording{}, fmt.Errorf("stop Recording: stop is already in progress")
	}
	active.stopping = true
	stoppedAt := w.scheduler.Now()
	w.recordingMu.Unlock()
	defer func() {
		w.recordingMu.Lock()
		if w.activeRecording == active {
			active.stopping = false
		}
		w.recordingMu.Unlock()
	}()
	if !active.stopped {
		stopTimer(active.warning)
		stopTimer(active.limit)
		if err := stopCapture(ctx, active.capture); err != nil {
			w.recordingMu.Lock()
			if w.activeRecording == active {
				w.activeRecording = nil
				w.captureFailure = err
			}
			w.recordingMu.Unlock()
			return recording.Recording{}, fmt.Errorf("stop Recording capture: %w", err)
		}
		w.recordingMu.Lock()
		if w.activeRecording == active {
			active.stopped = true
		}
		w.recordingMu.Unlock()
		active.recording.Duration = stoppedAt.Sub(active.recording.StartedAt)
		active.recording.Interrupted = false
	}
	if !active.promoted {
		active.recording.PendingPromotion = true
		if err := w.history.Save(ctx, active.recording); err != nil {
			return recording.Recording{}, fmt.Errorf("save completed Recording: %w", err)
		}
		if err := os.Rename(active.temporary, active.recording.AudioPath); err != nil {
			return recording.Recording{}, fmt.Errorf("promote completed Recording audio: %w", err)
		}
		active.promoted = true
	}
	active.recording.PendingPromotion = false
	if err := w.history.Save(ctx, active.recording); err != nil {
		return recording.Recording{}, fmt.Errorf("complete Recording promotion: %w", err)
	}
	completed := active.recording
	w.recordingMu.Lock()
	w.activeRecording = nil
	w.recordingMu.Unlock()
	w.resumeTranscription()
	if enabled, err := w.AutomaticTranscription(ctx); err == nil && enabled {
		_ = w.enqueueTranscription(ctx, completed)
	}
	if persisted, err := w.Recording(ctx, completed.ID); err == nil {
		completed = persisted
	}
	w.recordingMu.Lock()
	if active.limitReached {
		w.captureCompleted = &completed
	}
	w.recordingMu.Unlock()
	return completed, nil
}

// AutomaticTranscription reports whether completed Recordings are transcribed automatically.
func (w *Workflow) AutomaticTranscription(ctx context.Context) (bool, error) {
	value, found, err := w.history.Setting(ctx, "automatic_transcription")
	if err != nil {
		return false, fmt.Errorf("load automatic Transcription setting: %w", err)
	}
	return !found || value == "true", nil
}

// SetAutomaticTranscription persists whether completed Recordings are transcribed automatically.
func (w *Workflow) SetAutomaticTranscription(ctx context.Context, enabled bool) error {
	if err := w.history.SaveSetting(ctx, "automatic_transcription", strconv.FormatBool(enabled)); err != nil {
		return fmt.Errorf("save automatic Transcription setting: %w", err)
	}
	if enabled {
		w.wakeTranscriptionWorker()
	}
	return nil
}

// RequestTranscription manually creates or replaces a completed Recording's Transcription.
func (w *Workflow) RequestTranscription(ctx context.Context, id string) (recording.Recording, error) {
	detail, err := w.history.Recording(ctx, id)
	if err != nil {
		return recording.Recording{}, fmt.Errorf("load Recording for Transcription: %w", err)
	}
	if detail.PendingPromotion || detail.Interrupted || detail.AudioMissing {
		return recording.Recording{}, fmt.Errorf("request Transcription: Recording audio is not completed")
	}
	if err := w.enqueueTranscription(ctx, detail); err != nil {
		return recording.Recording{}, err
	}
	return w.Recording(ctx, id)
}

// CancelTranscription stops or removes a queued Transcription without affecting its Recording.
func (w *Workflow) CancelTranscription(ctx context.Context, id string) error {
	detail, err := w.history.Recording(ctx, id)
	if err != nil {
		return fmt.Errorf("load Recording to cancel Transcription: %w", err)
	}
	switch detail.TranscriptionStatus {
	case recording.TranscriptionPending:
		cancelled, err := w.history.CancelQueuedTranscription(ctx, id)
		if err != nil {
			return fmt.Errorf("cancel pending Transcription: %w", err)
		}
		if cancelled {
			return nil
		}
		// A worker may claim the queue entry between the initial read and this
		// conditional update. Re-read before reporting success to the user.
		detail, err = w.history.Recording(ctx, id)
		if err != nil {
			return fmt.Errorf("reload Transcription after queue cancellation: %w", err)
		}
		if detail.TranscriptionStatus != recording.TranscriptionRunning {
			return fmt.Errorf("cancel Transcription: work changed before cancellation (currently %s)", detail.TranscriptionStatus)
		}
		return w.cancelRunningTranscription(ctx, detail)
	case recording.TranscriptionRunning:
		return w.cancelRunningTranscription(ctx, detail)
	default:
		return fmt.Errorf("cancel Transcription: no queued Transcription for Recording %q", id)
	}
}

func (w *Workflow) cancelRunningTranscription(ctx context.Context, detail recording.Recording) error {
	cancelled, err := w.history.TransitionTranscriptionAttempt(ctx, detail.ID, detail.TranscriptionAttempt, recording.TranscriptionCancelled, recording.ProcessingFailureCancelled, "Transcription was cancelled.")
	if err != nil {
		return fmt.Errorf("cancel running Transcription: %w", err)
	}
	if !cancelled {
		return fmt.Errorf("cancel Transcription: work changed before cancellation")
	}
	w.processingMu.Lock()
	if w.runningID == detail.ID && w.runningAttempt == detail.TranscriptionAttempt && w.runningCancel != nil {
		w.runningCancelledByUser = true
		w.runningCancel()
	}
	w.processingMu.Unlock()
	return nil
}

// Recording returns a detail view including its full timestamped Transcription.
func (w *Workflow) Recording(ctx context.Context, id string) (recording.Recording, error) {
	detail, err := w.history.Recording(ctx, id)
	if err != nil {
		return recording.Recording{}, fmt.Errorf("load workflow Recording: %w", err)
	}
	return detail, nil
}

func (w *Workflow) enqueueTranscription(ctx context.Context, completed recording.Recording) error {
	if completed.TranscriptionStatus == recording.TranscriptionRunning || completed.TranscriptionStatus == recording.TranscriptionPending {
		if completed.TranscriptionStatus == recording.TranscriptionPending {
			w.wakeTranscriptionWorker()
		}
		return nil
	}
	if err := w.history.QueueTranscription(ctx, completed.ID); err != nil {
		return fmt.Errorf("queue Transcription for Recording %q: %w", completed.ID, err)
	}
	w.wakeTranscriptionWorker()
	return nil
}

func (w *Workflow) transcribe(ctx context.Context, completed recording.Recording) ([]recording.Segment, error) {
	segments, err := w.transcriber.Transcribe(ctx, completed.AudioPath)
	if err != nil {
		return nil, fmt.Errorf("transcribe Recording %q: %w", completed.ID, err)
	}
	persisted := make([]recording.Segment, len(segments))
	for index, segment := range segments {
		persisted[index] = recording.Segment{Start: segment.Start, End: segment.End, Text: segment.Text}
	}
	return persisted, nil
}

func (w *Workflow) runTranscriptionWorker(ctx context.Context) {
	defer w.workerWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.workerWake:
		}
		for {
			if ctx.Err() != nil {
				return
			}
			w.processingMu.Lock()
			paused := w.processingPaused
			w.processingMu.Unlock()
			if paused {
				break
			}
			completed, err := w.history.ClaimNextPendingTranscription(ctx)
			if err != nil {
				// Persistence failures cannot safely be shown as a Recording failure here;
				// leave durable work pending for the next explicit wake or restart.
				break
			}
			if completed == nil {
				break
			}
			if _, err := os.Stat(completed.AudioPath); err != nil {
				_, _ = w.history.TransitionTranscriptionAttempt(context.Background(), completed.ID, completed.TranscriptionAttempt, recording.TranscriptionFailed, recording.ProcessingFailureExecution, "Recording audio is unavailable. Restore the audio file and try again.")
				continue
			}
			claimed, err := w.history.Recording(ctx, completed.ID)
			if err != nil || claimed.TranscriptionStatus != recording.TranscriptionRunning {
				continue
			}
			w.processingMu.Lock()
			if w.processingPaused {
				w.processingMu.Unlock()
				_, _ = w.history.TransitionTranscriptionAttempt(context.Background(), completed.ID, completed.TranscriptionAttempt, recording.TranscriptionPending, "", "")
				break
			}
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			w.runningCancel, w.runningDone, w.runningID, w.runningAttempt, w.runningCancelledByUser = cancel, done, completed.ID, completed.TranscriptionAttempt, false
			w.processingMu.Unlock()

			segments, runErr := w.transcribe(runCtx, *completed)
			w.processingMu.Lock()
			cancelledByUser := w.runningCancelledByUser
			w.processingMu.Unlock()
			if runCtx.Err() != nil && cancelledByUser {
				_, _ = w.history.TransitionTranscriptionAttempt(context.Background(), completed.ID, completed.TranscriptionAttempt, recording.TranscriptionCancelled, recording.ProcessingFailureCancelled, "Transcription was cancelled.")
			} else if runCtx.Err() != nil {
				_, _ = w.history.TransitionTranscriptionAttempt(context.Background(), completed.ID, completed.TranscriptionAttempt, recording.TranscriptionPending, "", "")
			} else if runErr != nil {
				category, detail := transcriptionFailure(runErr)
				_, _ = w.history.TransitionTranscriptionAttempt(context.Background(), completed.ID, completed.TranscriptionAttempt, recording.TranscriptionFailed, category, detail)
			} else {
				_, _ = w.history.CompleteTranscriptionAttempt(runCtx, completed.ID, completed.TranscriptionAttempt, segments)
			}
			cancel()
			w.processingMu.Lock()
			w.runningCancel, w.runningDone, w.runningID, w.runningAttempt, w.runningCancelledByUser = nil, nil, "", 0, false
			close(done)
			w.processingMu.Unlock()
		}
	}
}

func transcriptionFailure(err error) (recording.ProcessingFailureCategory, string) {
	// External tool errors may include audio paths or spoken text. Only stable,
	// user-safe outcomes cross this boundary; no raw diagnostic is persisted or logged.
	if strings.Contains(err.Error(), "not configured") || strings.Contains(err.Error(), "configuration") {
		return recording.ProcessingFailureConfiguration, "Local transcription is not configured. Check the whisper.cpp executable and model paths."
	}
	return recording.ProcessingFailureExecution, "Transcription could not be completed. Try again."
}

func (w *Workflow) wakeTranscriptionWorker() {
	select {
	case w.workerWake <- struct{}{}:
	default:
	}
}

func (w *Workflow) pauseTranscription() {
	w.processingMu.Lock()
	w.processingPaused = true
	w.processingMu.Unlock()
}

func (w *Workflow) resumeTranscription() {
	w.processingMu.Lock()
	w.processingPaused = false
	w.processingMu.Unlock()
	w.wakeTranscriptionWorker()
}

func (w *Workflow) cancelAndAwaitTranscription() {
	w.processingMu.Lock()
	cancel, done := w.runningCancel, w.runningDone
	if cancel != nil {
		cancel()
	}
	w.processingMu.Unlock()
	if done != nil {
		<-done
	}
}

// CaptureState returns state that the UI can poll while a Recording is active.
func (w *Workflow) CaptureState() CaptureState {
	w.recordingMu.Lock()
	defer w.recordingMu.Unlock()
	state := CaptureState{}
	if w.activeRecording != nil {
		state.Active = &ActiveRecording{ID: w.activeRecording.recording.ID, StartedAt: w.activeRecording.recording.StartedAt, Source: w.activeRecording.source}
	}
	if w.activeRecording != nil && w.activeRecording.warned {
		state.Warning = fmt.Sprintf("Recording will stop in %s.", w.activeRecording.warningDuration)
	}
	if w.captureFailure != nil {
		state.Failure = w.captureFailure.Error()
	}
	if w.captureCompleted != nil {
		completed := *w.captureCompleted
		state.Completed = &completed
	}
	return state
}

func (w *Workflow) watchCapture(active *activeRecording) {
	if err := active.capture.Wait(); err != nil {
		w.recordingMu.Lock()
		if w.activeRecording == active && !active.stopping {
			stopTimer(active.warning)
			stopTimer(active.limit)
			w.activeRecording = nil
			w.captureFailure = err
		}
		w.recordingMu.Unlock()
	}
}

func (w *Workflow) warnRecording(active *activeRecording) {
	w.recordingMu.Lock()
	defer w.recordingMu.Unlock()
	if w.activeRecording == active && !active.stopping {
		active.warned = true
	}
}

func (w *Workflow) stopAtLimit(active *activeRecording) {
	w.recordingMu.Lock()
	if w.activeRecording != active || active.stopping {
		w.recordingMu.Unlock()
		return
	}
	active.limitReached = true
	w.recordingMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), w.limitStopTimeout)
	defer cancel()
	if _, err := w.StopRecording(ctx); err != nil {
		w.recordingMu.Lock()
		if w.activeRecording == active {
			w.activeRecording = nil
			w.captureFailure = err
		}
		w.recordingMu.Unlock()
	}
}

func stopTimer(timer Timer) {
	if timer != nil {
		timer.Stop()
	}
}

func stopCapture(ctx context.Context, capture audio.Capture) error {
	stopped := make(chan error, 1)
	go func() { stopped <- capture.Stop(ctx) }()
	select {
	case err := <-stopped:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// History returns Recordings ordered from newest to oldest.
func (w *Workflow) History(ctx context.Context) ([]recording.Recording, error) {
	history, err := w.history.History(ctx)
	if err != nil {
		return nil, fmt.Errorf("load workflow Recording history: %w", err)
	}

	return history, nil
}

// RenameRecording changes the user-editable title of a completed Recording.
func (w *Workflow) RenameRecording(ctx context.Context, id, title string) error {
	if title == "" {
		return fmt.Errorf("rename Recording: title is required")
	}
	if err := w.history.Rename(ctx, id, title); err != nil {
		return fmt.Errorf("rename Recording: %w", err)
	}
	return nil
}

// Close releases the workflow's local resources.
func (w *Workflow) Close() error {
	w.cancelAndAwaitTranscription()
	w.workerCancel()
	w.wakeTranscriptionWorker()
	w.workerWG.Wait()
	w.recordingMu.Lock()
	if w.activeRecording != nil {
		active := w.activeRecording
		if active.stopped {
			w.activeRecording = nil
			w.recordingMu.Unlock()
			if err := w.history.Close(); err != nil {
				return fmt.Errorf("close application workflow: %w", err)
			}
			return nil
		}
		active.stopping = true
		stopTimer(active.warning)
		stopTimer(active.limit)
		w.recordingMu.Unlock()
		if err := active.capture.Stop(context.Background()); err != nil {
			_ = os.Remove(active.temporary)
			w.recordingMu.Lock()
			w.activeRecording = nil
			w.recordingMu.Unlock()
			return fmt.Errorf("stop active Recording while closing: %w", err)
		}
		_ = os.Remove(active.temporary)
		w.recordingMu.Lock()
		w.activeRecording = nil
	}
	w.recordingMu.Unlock()
	if err := w.history.Close(); err != nil {
		return fmt.Errorf("close application workflow: %w", err)
	}

	return nil
}
