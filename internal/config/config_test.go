package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveValidatesAndPersistsLocalProcessingConfiguration(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	executable := filepath.Join(t.TempDir(), "whisper-cli")
	model := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}

	want := Processing{WhisperExecutable: executable, WhisperModel: model, WhisperThreads: 4, OllamaEndpoint: "http://127.0.0.1:11434", OllamaModel: "llama3.2"}
	if err := Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Errorf("Load() = %#v, want %#v", got, want)
	}
}

func TestSaveRejectsInvalidWhisperModelPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	executable := filepath.Join(t.TempDir(), "whisper-cli")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := Save(context.Background(), Processing{WhisperExecutable: executable, WhisperModel: filepath.Join(t.TempDir(), "missing.bin"), WhisperThreads: 3})
	if err == nil {
		t.Fatal("Save() error = nil, want invalid model path")
	}
}
