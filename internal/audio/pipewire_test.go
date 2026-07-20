package audio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestSourcesBoundsPipeWireAndPulseAudioDiscovery(t *testing.T) {
	var deadlines []time.Time
	adapter := pipeWire{
		lookPath: func(string) error { return nil },
		run: func(ctx context.Context, name string, _ ...string) ([]byte, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("discovery command context has no deadline")
			}
			deadlines = append(deadlines, deadline)
			if name == "pw-dump" {
				return nil, errors.New("PipeWire unavailable")
			}
			return []byte("0\toutput.monitor\tPipeWire\n"), nil
		},
	}

	started := time.Now()
	sources, err := adapter.Sources(context.Background())
	if err != nil {
		t.Fatalf("Sources() error = %v", err)
	}
	if len(sources) != 1 || sources[0].ID != "output.monitor" {
		t.Fatalf("Sources() = %#v, want PulseAudio fallback source", sources)
	}
	if len(deadlines) != 2 {
		t.Fatalf("discovery command count = %d, want 2", len(deadlines))
	}
	for _, deadline := range deadlines {
		if deadline.Sub(started) > discoveryTimeout+100*time.Millisecond {
			t.Errorf("discovery deadline = %v after start, want at most %v", deadline.Sub(started), discoveryTimeout)
		}
	}
}

func TestSourcesExplainsHowToRecoverWhenNoActivityMeterIsAvailable(t *testing.T) {
	adapter := pipeWire{
		lookPath: func(name string) error {
			if name == "pw-dump" {
				return nil
			}
			return errors.New("not found")
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte(`[{"info":{"props":{"media.class":"Audio/Source","node.name":"output.monitor"}}}]`), nil
		},
	}

	sources, err := adapter.Sources(context.Background())
	if len(sources) != 1 {
		t.Errorf("Sources() = %#v, want discovered system-output source", sources)
	}
	if err == nil {
		t.Fatal("Sources() error = nil, want activity-meter recovery guidance")
	}
	if !strings.Contains(err.Error(), "pw-cat") || !strings.Contains(err.Error(), "parec") {
		t.Errorf("Sources() error = %q, want pw-cat and parec recovery guidance", err)
	}
}

func TestStartExplainsWhenFFmpegIsUnavailable(t *testing.T) {
	adapter := pipeWire{
		lookPath: func(name string) error {
			if name == "ffmpeg" {
				return errors.New("not found")
			}
			return nil
		},
	}

	_, err := adapter.Start(context.Background(), Source{ID: "speakers.monitor"}, t.TempDir()+"/recording.opus")
	if err == nil {
		t.Fatal("Start() error = nil, want unavailable FFmpeg guidance")
	}
	if !strings.Contains(err.Error(), "ffmpeg") {
		t.Errorf("Start() error = %q, want FFmpeg guidance", err)
	}
}

func TestDrainCaptureCopiesAllPCMBeforeReapingInput(t *testing.T) {
	input := bytes.NewBufferString("all buffered PCM")
	reader, output := io.Pipe()
	var copied bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, err := copied.ReadFrom(reader)
		readDone <- err
	}()
	waitCalled := false
	var readErr error

	if err := drainCapture(input, output, func() error {
		waitCalled = true
		readErr = <-readDone
		if readErr != nil {
			return readErr
		}
		if got, want := copied.String(), "all buffered PCM"; got != want {
			t.Errorf("PCM copied before input Wait = %q, want %q", got, want)
		}
		return nil
	}); err != nil {
		t.Fatalf("drainCapture() error = %v", err)
	}
	if !waitCalled {
		t.Error("input Wait was not called after draining PCM")
	}
}
