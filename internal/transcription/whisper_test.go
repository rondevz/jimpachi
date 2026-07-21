package transcription

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWhisperConvertsRecordingToWAVBeforeTranscribing(t *testing.T) {
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	whisper := filepath.Join(dir, "whisper-cli")
	ffmpegArguments := filepath.Join(dir, "ffmpeg-arguments")
	wavMarker := filepath.Join(dir, "whisper-wav-path")
	t.Setenv("JIMPACHI_TEST_FFMPEG_ARGUMENTS", ffmpegArguments)
	t.Setenv("JIMPACHI_TEST_WAV_PATH", wavMarker)
	writeExecutable(t, ffmpeg, `#!/bin/sh
printf '%s\n' "$@" > "$JIMPACHI_TEST_FFMPEG_ARGUMENTS"
for argument do output="$argument"; done
printf wav > "$output"
`)
	writeExecutable(t, whisper, `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -f) input="$2"; shift 2 ;;
    -of) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
case "$input" in *.wav) ;; *) exit 2 ;; esac
test -f "$input" || exit 3
printf '%s' "$input" > "$JIMPACHI_TEST_WAV_PATH"
printf '{"transcription":[]}' > "${output}.json"
`)
	audioPath := filepath.Join(dir, "recording.opus")
	if err := os.WriteFile(audioPath, []byte("opus"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (Whisper{Executable: whisper, Model: filepath.Join(dir, "model.bin"), Threads: 3, ffmpeg: ffmpeg}).Transcribe(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	arguments, err := os.ReadFile(ffmpegArguments)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-i\n" + audioPath, "-ar\n16000", "-ac\n1"} {
		if !strings.Contains(string(arguments), want) {
			t.Errorf("FFmpeg arguments = %q, want %q", arguments, want)
		}
	}
	wavPath, err := os.ReadFile(wavMarker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(wavPath)); !os.IsNotExist(err) {
		t.Errorf("temporary WAV after Transcribe() = %v, want removed", err)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestParseJSONReadsTimestampedSegmentsFromWhisperOutput(t *testing.T) {
	segments, err := parseJSON([]byte(`{"result":{"transcription":[{"timestamps":{"from":"00:00:01,250","to":"00:00:03,500"},"text":" Deploy the service. "}]}}`))
	if err != nil {
		t.Fatalf("parseJSON() error = %v", err)
	}
	if got, want := len(segments), 1; got != want {
		t.Fatalf("segments = %#v, want one segment", segments)
	}
	if got, want := segments[0], (Segment{Start: 1250 * time.Millisecond, End: 3500 * time.Millisecond, Text: "Deploy the service."}); got != want {
		t.Errorf("segment = %#v, want %#v", got, want)
	}
}
