// Package config owns Jimpachi's local processing configuration.
package config

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Processing configures the local tools used after Recording capture.
type Processing struct {
	WhisperExecutable string
	WhisperModel      string
	WhisperThreads    int
	OllamaEndpoint    string
	OllamaModel       string
}

// Load reads the optional local processing configuration.
func Load(ctx context.Context) (Processing, error) {
	if err := ctx.Err(); err != nil {
		return Processing{}, err
	}
	contents, err := os.ReadFile(path())
	if os.IsNotExist(err) {
		return Processing{}, nil
	}
	if err != nil {
		return Processing{}, fmt.Errorf("read local processing configuration: %w", err)
	}
	var result Processing
	section := ""
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.TrimSpace(parts[0]), strings.Trim(strings.TrimSpace(parts[1]), "\"")
		switch section + "." + key {
		case "whisper.executable":
			result.WhisperExecutable = value
		case "whisper.model":
			result.WhisperModel = value
		case "whisper.threads":
			threads, err := strconv.Atoi(value)
			if err != nil {
				return Processing{}, fmt.Errorf("read local processing configuration: whisper threads must be a positive integer")
			}
			result.WhisperThreads = threads
		case "ollama.endpoint":
			result.OllamaEndpoint = value
		case "ollama.model":
			result.OllamaModel = value
		}
	}
	return result, nil
}

// Save validates and persists local processing configuration.
func Save(ctx context.Context, processing Processing) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := Validate(processing); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path()), 0o755); err != nil {
		return fmt.Errorf("create local processing configuration directory: %w", err)
	}
	contents := fmt.Sprintf("[whisper]\nexecutable = %q\nmodel = %q\nthreads = %d\n\n[ollama]\nendpoint = %q\nmodel = %q\n", processing.WhisperExecutable, processing.WhisperModel, processing.WhisperThreads, processing.OllamaEndpoint, processing.OllamaModel)
	if err := os.WriteFile(path(), []byte(contents), 0o600); err != nil {
		return fmt.Errorf("write local processing configuration: %w", err)
	}
	return nil
}

// Snapshot preserves the exact current configuration for a compensating restore.
func Snapshot(ctx context.Context) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	contents, err := os.ReadFile(path())
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read local processing configuration: %w", err)
	}
	return contents, true, nil
}

// Restore replaces the configuration with a previously captured Snapshot.
func Restore(ctx context.Context, contents []byte, exists bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !exists {
		if err := os.Remove(path()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove local processing configuration: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path()), 0o755); err != nil {
		return fmt.Errorf("create local processing configuration directory: %w", err)
	}
	if err := os.WriteFile(path(), contents, 0o600); err != nil {
		return fmt.Errorf("restore local processing configuration: %w", err)
	}
	return nil
}

// Validate confirms configured local dependencies are safe and usable.
func Validate(processing Processing) error {
	whisperConfigured := processing.WhisperExecutable != "" || processing.WhisperModel != "" || processing.WhisperThreads != 0
	if whisperConfigured {
		if processing.WhisperExecutable == "" || processing.WhisperModel == "" {
			return fmt.Errorf("validate local processing configuration: whisper executable and model paths are both required")
		}
		if processing.WhisperThreads < 1 {
			return fmt.Errorf("validate local processing configuration: whisper threads must be a positive integer")
		}
		if err := executable(processing.WhisperExecutable); err != nil {
			return fmt.Errorf("validate local processing configuration: %w", err)
		}
		if err := regularFile(processing.WhisperModel, "whisper model"); err != nil {
			return fmt.Errorf("validate local processing configuration: %w", err)
		}
	}
	ollamaConfigured := processing.OllamaEndpoint != "" || processing.OllamaModel != ""
	if ollamaConfigured {
		if processing.OllamaEndpoint == "" || processing.OllamaModel == "" {
			return fmt.Errorf("validate local processing configuration: Ollama endpoint and model are both required")
		}
		endpoint, err := url.Parse(processing.OllamaEndpoint)
		if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() == "" {
			return fmt.Errorf("validate local processing configuration: Ollama endpoint must be an HTTP loopback URL")
		}
		if host := endpoint.Hostname(); !strings.EqualFold(host, "localhost") {
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return fmt.Errorf("validate local processing configuration: Ollama endpoint must be a loopback URL")
			}
		}
	}
	return nil
}

func executable(value string) error {
	info, err := os.Stat(value)
	if err != nil {
		return fmt.Errorf("whisper executable path: %w", err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("whisper executable path must be an executable file")
	}
	return nil
}

func regularFile(value, name string) error {
	info, err := os.Stat(value)
	if err != nil {
		return fmt.Errorf("%s path: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s path must be a regular file", name)
	}
	return nil
}

func path() string {
	if home := os.Getenv("XDG_CONFIG_HOME"); home != "" {
		return filepath.Join(home, "jimpachi", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "jimpachi", "config.toml")
	}
	return filepath.Join(home, ".config", "jimpachi", "config.toml")
}
