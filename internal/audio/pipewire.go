package audio

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// New returns an adapter for PipeWire, with PulseAudio-compatible fallbacks.
func New() Adapter {
	return pipeWire{
		lookPath: func(name string) error {
			_, err := exec.LookPath(name)
			return err
		},
		run: commandOutput,
	}
}

const discoveryTimeout = 2 * time.Second
const recoveryProbeTimeout = 2 * time.Second

type outputRunner func(context.Context, string, ...string) ([]byte, error)

type pipeWire struct {
	lookPath func(string) error
	run      outputRunner
}

func (p pipeWire) Sources(ctx context.Context) ([]Source, error) {
	var failures []string
	if err := p.lookPath("pw-dump"); err == nil {
		sources, err := pipeWireSources(ctx, p.discovery)
		if err == nil && len(sources) > 0 {
			return sources, p.meterAvailability()
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("PipeWire: %v", err))
		} else {
			failures = append(failures, "PipeWire: no system-output monitors found")
		}
	} else {
		failures = append(failures, "PipeWire command pw-dump is unavailable")
	}

	sources, err := pulseSources(ctx, p.discovery)
	if err == nil && len(sources) > 0 {
		return sources, p.meterAvailability()
	}
	if err != nil {
		failures = append(failures, fmt.Sprintf("PulseAudio fallback: %v", err))
	} else {
		failures = append(failures, "PulseAudio fallback: no system-output monitors found")
	}

	return nil, fmt.Errorf("%s", strings.Join(failures, "; "))
}

func (p pipeWire) discovery(ctx context.Context, command string, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	return p.run(ctx, command, arguments...)
}

func commandOutput(ctx context.Context, command string, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, command, arguments...).Output()
}

func (p pipeWire) meterAvailability() error {
	if p.lookPath("pw-cat") == nil || p.lookPath("parec") == nil {
		return nil
	}

	return fmt.Errorf("Audio activity meter is unavailable: make pw-cat or parec available, then restart Jimpachi")
}

func pipeWireSources(ctx context.Context, run outputRunner) ([]Source, error) {
	output, err := run(ctx, "pw-dump")
	if err != nil {
		return nil, fmt.Errorf("run pw-dump: %w", err)
	}

	var objects []struct {
		Info struct {
			Props map[string]any `json:"props"`
		} `json:"info"`
	}
	if err := json.Unmarshal(output, &objects); err != nil {
		return nil, fmt.Errorf("parse pw-dump output: %w", err)
	}

	var monitors []Source
	for _, object := range objects {
		props := object.Info.Props
		mediaClass, _ := props["media.class"].(string)
		id, _ := props["node.name"].(string)
		if id == "" {
			continue
		}
		name, _ := props["node.description"].(string)
		if name == "" {
			name = id
		}
		if mediaClass == "Audio/Source" && strings.HasSuffix(id, ".monitor") {
			monitors = append(monitors, Source{ID: id, Name: name})
		}
	}

	return monitors, nil
}

func pulseSources(ctx context.Context, run outputRunner) ([]Source, error) {
	output, err := run(ctx, "pactl", "list", "short", "sources")
	if err != nil {
		return nil, fmt.Errorf("run pactl list short sources: %w", err)
	}

	var sources []Source
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasSuffix(fields[1], ".monitor") {
			continue
		}
		sources = append(sources, Source{ID: fields[1], Name: fields[1]})
	}

	return sources, nil
}

func (p pipeWire) Activity(ctx context.Context, source Source) (float64, error) {
	if err := p.validateSystemOutputSource(ctx, source); err != nil {
		return 0, err
	}
	if p.lookPath("pw-cat") == nil {
		level, pipeWireErr := activity(ctx, "pw-cat", "--record", "--target", source.ID, "--format", "s16", "--rate", "48000", "--channels", "1", "-")
		if pipeWireErr == nil {
			return level, nil
		}
		if p.lookPath("parec") != nil {
			return 0, fmt.Errorf("measure with pw-cat: %w", pipeWireErr)
		}
	}
	return activity(ctx, "parec", "--device", source.ID, "--raw", "--format=s16le", "--rate=48000", "--channels=1")
}

// Start captures a monitor as raw PCM and gives FFmpeg sole responsibility for
// encoding the final, portable mono Opus file.
func (p pipeWire) Start(ctx context.Context, source Source, path string) (Capture, error) {
	if err := p.lookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("start Audio capture: ffmpeg is unavailable: %w", err)
	}
	if err := p.validateSystemOutputSource(ctx, source); err != nil {
		return nil, fmt.Errorf("start Audio capture: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create Recording directory: %w", err)
	}

	command := "pw-cat"
	arguments := []string{"--record", "--target", source.ID, "--format", "s16", "--rate", "48000", "--channels", "1", "-"}
	if p.lookPath(command) != nil {
		if err := p.lookPath("parec"); err != nil {
			return nil, fmt.Errorf("start Audio capture: pw-cat and parec are unavailable: %w", err)
		}
		command = "parec"
		arguments = []string{"--device", source.ID, "--raw", "--format=s16le", "--rate=48000", "--channels=1"}
	}

	captureCtx, cancelInput := context.WithCancel(ctx)
	input := exec.CommandContext(captureCtx, command, arguments...)
	stdout, err := input.StdoutPipe()
	if err != nil {
		cancelInput()
		return nil, fmt.Errorf("open %s audio stream: %w", command, err)
	}
	if err := input.Start(); err != nil {
		cancelInput()
		return nil, fmt.Errorf("start %s audio stream: %w", command, err)
	}

	encoderCtx, cancelEncoder := context.WithCancel(ctx)
	encoder := exec.CommandContext(encoderCtx, "ffmpeg", "-y", "-f", "s16le", "-ar", "48000", "-ac", "1", "-i", "pipe:0", "-c:a", "libopus", "-b:a", "32k", path)
	encoderInput, encoderOutput := io.Pipe()
	encoder.Stdin = encoderInput
	if err := encoder.Start(); err != nil {
		_ = encoderInput.Close()
		_ = encoderOutput.Close()
		cancelEncoder()
		cancelInput()
		_ = input.Wait()
		return nil, fmt.Errorf("start FFmpeg encoder: %w", err)
	}

	capture := &processCapture{cancelInput: cancelInput, cancelEncoder: cancelEncoder, input: input, encoder: encoder, stdout: stdout, encoderOutput: encoderOutput, done: make(chan struct{})}
	go capture.supervise()
	return capture, nil
}

func (p pipeWire) validateSystemOutputSource(ctx context.Context, source Source) error {
	if source.ID == "" {
		return fmt.Errorf("Audio source path is required")
	}
	sources, err := p.Sources(ctx)
	if err != nil {
		return fmt.Errorf("validate system-output source: %w", err)
	}
	for _, candidate := range sources {
		if candidate.ID == source.ID {
			return nil
		}
	}
	return fmt.Errorf("Audio source %q is not a system-output monitor", source.ID)
}

// Playable reports whether FFmpeg can decode captured audio left by an interrupted process.
func (p pipeWire) Playable(ctx context.Context, path string) (bool, error) {
	if err := p.lookPath("ffprobe"); err != nil {
		return false, fmt.Errorf("check interrupted Recording audio: ffprobe is unavailable: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, recoveryProbeTimeout)
	defer cancel()
	if _, err := p.run(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1", path); err != nil {
		return false, fmt.Errorf("probe interrupted Recording audio: %w", err)
	}
	return true, nil
}

type processCapture struct {
	cancelInput   context.CancelFunc
	cancelEncoder context.CancelFunc
	input         *exec.Cmd
	encoder       *exec.Cmd
	stdout        io.ReadCloser
	encoderOutput *io.PipeWriter
	once          sync.Once
	err           error
	done          chan struct{}
	mu            sync.Mutex
	stopping      bool
}

func (c *processCapture) Stop(ctx context.Context) error {
	c.mu.Lock()
	c.stopping = true
	c.mu.Unlock()
	c.once.Do(func() {
		// Cancel the source first so FFmpeg receives EOF, then wait for both
		// processes to prevent capture children surviving a stopped Recording.
		c.cancelInput()
		select {
		case <-c.done:
		case <-ctx.Done():
			c.cancelEncoder()
			<-c.done
		}
	})
	return c.Wait()
}

func (c *processCapture) Wait() error {
	<-c.done
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *processCapture) supervise() {
	sourceDone := make(chan error, 1)
	encoderDone := make(chan error, 1)
	go func() {
		// Drain the PCM pipe before Wait closes it, so FFmpeg receives every
		// captured sample that was already buffered when capture stops.
		sourceDone <- drainCapture(c.stdout, c.encoderOutput, c.input.Wait)
	}()
	go func() { encoderDone <- c.encoder.Wait() }()

	var inputErr, encoderErr error
	inputFinished := false
	select {
	case inputErr = <-sourceDone:
		inputFinished = true
	case encoderErr = <-encoderDone:
	}
	c.mu.Lock()
	stopping := c.stopping
	c.mu.Unlock()
	if !stopping {
		c.cancelInput()
		c.cancelEncoder()
	}
	if inputFinished {
		encoderErr = <-encoderDone
	} else {
		inputErr = <-sourceDone
	}
	c.mu.Lock()
	if stopping {
		if !expectedStopError(inputErr) || !expectedStopError(encoderErr) {
			c.err = fmt.Errorf("stop Audio capture: source: %v; FFmpeg: %v", inputErr, encoderErr)
		}
	} else {
		c.err = fmt.Errorf("Audio capture ended unexpectedly: source: %v; FFmpeg: %v", inputErr, encoderErr)
	}
	c.mu.Unlock()
	close(c.done)
}

func drainCapture(input io.Reader, output *io.PipeWriter, wait func() error) error {
	_, copyErr := io.Copy(output, input)
	_ = output.CloseWithError(copyErr)
	inputErr := wait()
	if copyErr != nil {
		return fmt.Errorf("copy Audio capture stream: %w", copyErr)
	}
	return inputErr
}

func expectedStopError(err error) bool {
	return err == nil || errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "signal: killed")
}

func activity(ctx context.Context, command string, arguments ...string) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	process := exec.CommandContext(ctx, command, arguments...)
	stdout, err := process.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("open %s audio stream: %w", command, err)
	}
	if err := process.Start(); err != nil {
		return 0, fmt.Errorf("start %s audio stream: %w", command, err)
	}
	defer process.Wait()

	data := make([]byte, 4800)
	count, err := io.ReadFull(stdout, data)
	if err != nil && count < 2 {
		return 0, fmt.Errorf("read %s audio stream: %w", command, err)
	}

	var sum float64
	for index := 0; index+1 < count; index += 2 {
		sample := int16(binary.LittleEndian.Uint16(data[index : index+2]))
		value := float64(sample) / 32768
		sum += value * value
	}
	if count < 2 {
		return 0, nil
	}

	return math.Sqrt(sum / float64(count/2)), nil
}
