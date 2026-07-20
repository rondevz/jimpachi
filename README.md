# Jimpachi

Jimpachi is a terminal application for recording system audio, transcribing recordings locally, and optionally creating local AI summaries through Ollama.

## Prerequisites

- Go 1.24 or newer
- PipeWire or PulseAudio for system-audio capture (planned)
- `whisper.cpp` for local transcription (planned)
- Ollama for optional local summaries (planned)

Install Go on Ubuntu, then open a new shell:

```sh
sudo snap install go --classic
```

## Run

```sh
go mod tidy
go run .
```

Press `q` to quit the initial TUI.

## Planned storage

By default, Jimpachi will store its database and recordings beneath `~/.local/share/jimpachi/` and read configuration from `~/.config/jimpachi/config.toml`.
