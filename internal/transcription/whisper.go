// Package transcription adapts local speech-to-text tools to Jimpachi.
package transcription

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"jimpachi/internal/config"
)

// Segment is one timestamped span of a Transcription.
type Segment struct {
	Start time.Duration
	End   time.Duration
	Text  string
}

// Whisper invokes a configured local whisper.cpp executable.
type Whisper struct {
	Executable string
	Model      string
	Threads    int
}

// LoadConfiguredWhisper reads the optional local whisper.cpp configuration.
func LoadConfiguredWhisper() (Whisper, error) {
	configured, err := config.Load(context.Background())
	if err != nil {
		return Whisper{}, fmt.Errorf("read whisper.cpp configuration: %w", err)
	}
	return Whisper{Executable: configured.WhisperExecutable, Model: configured.WhisperModel, Threads: configured.WhisperThreads}, nil
}

// Transcribe produces timestamped text with whisper.cpp language auto-detection.
func (w Whisper) Transcribe(ctx context.Context, audioPath string) ([]Segment, error) {
	if w.Executable == "" {
		return nil, fmt.Errorf("run whisper.cpp: executable path is not configured")
	}
	if w.Model == "" {
		return nil, fmt.Errorf("run whisper.cpp: model path is not configured")
	}
	threads := w.Threads
	if threads == 0 {
		threads = 3
	}
	outputDir, err := os.MkdirTemp("", "jimpachi-whisper-")
	if err != nil {
		return nil, fmt.Errorf("create whisper.cpp output directory: %w", err)
	}
	defer os.RemoveAll(outputDir)
	outputBase := filepath.Join(outputDir, "transcription")

	// whisper.cpp writes JSON to <output-base>.json, not stdout. -l auto preserves
	// its language detection instead of forcing the user's interface language.
	command := exec.CommandContext(ctx, w.Executable, "-m", w.Model, "-f", audioPath, "-of", outputBase, "-oj", "-l", "auto", "-t", fmt.Sprint(threads))
	// Discard tool output: it can contain transcription text or local paths, neither
	// of which belongs in a user-visible error or a warning/error log.
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("run whisper.cpp: %w", err)
	}

	contents, err := os.ReadFile(outputBase + ".json")
	if err != nil {
		return nil, fmt.Errorf("read whisper.cpp JSON output: %w", err)
	}
	segments, err := parseJSON(contents)
	if err != nil {
		return nil, fmt.Errorf("parse whisper.cpp JSON output: %w", err)
	}
	return segments, nil
}

type jsonOutput struct {
	Transcription []jsonSegment `json:"transcription"`
	Result        struct {
		Transcription []jsonSegment `json:"transcription"`
	} `json:"result"`
}

type jsonSegment struct {
	Timestamps struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"timestamps"`
	Text string `json:"text"`
}

func parseJSON(contents []byte) ([]Segment, error) {
	var output jsonOutput
	// whisper.cpp has emitted both a top-level transcription array and a result
	// object across releases; accepting both keeps parsing inside this adapter.
	if err := json.Unmarshal(contents, &output); err != nil {
		return nil, err
	}
	entries := output.Transcription
	if len(entries) == 0 {
		entries = output.Result.Transcription
	}
	segments := make([]Segment, 0, len(entries))
	for _, entry := range entries {
		start, err := parseTimestamp(entry.Timestamps.From)
		if err != nil {
			return nil, fmt.Errorf("parse segment start %q: %w", entry.Timestamps.From, err)
		}
		end, err := parseTimestamp(entry.Timestamps.To)
		if err != nil {
			return nil, fmt.Errorf("parse segment end %q: %w", entry.Timestamps.To, err)
		}
		segments = append(segments, Segment{Start: start, End: end, Text: strings.TrimSpace(entry.Text)})
	}
	return segments, nil
}

func parseTimestamp(value string) (time.Duration, error) {
	value = strings.Replace(value, ",", ".", 1)
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("expected HH:MM:SS.mmm")
	}
	hours, err := time.ParseDuration(parts[0] + "h")
	if err != nil {
		return 0, err
	}
	minutes, err := time.ParseDuration(parts[1] + "m")
	if err != nil {
		return 0, err
	}
	seconds, err := time.ParseDuration(parts[2] + "s")
	if err != nil {
		return 0, err
	}
	return hours + minutes + seconds, nil
}
