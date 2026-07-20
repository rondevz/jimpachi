package audio

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strings"
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

	var sources []Source
	for _, object := range objects {
		props := object.Info.Props
		mediaClass, _ := props["media.class"].(string)
		id, _ := props["node.name"].(string)
		if mediaClass != "Audio/Source" || !strings.HasSuffix(id, ".monitor") {
			continue
		}
		name, _ := props["node.description"].(string)
		if name == "" {
			name = id
		}
		sources = append(sources, Source{ID: id, Name: name})
	}

	return sources, nil
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
	if source.ID == "" {
		return 0, fmt.Errorf("Audio source path is required")
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
