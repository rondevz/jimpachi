# Jimpachi

Jimpachi is a terminal application for recording system audio, transcribing recordings locally, and optionally creating local AI summaries through Ollama.

## Prerequisites

- Go 1.24 or newer
- PipeWire (`pw-dump`, `pw-cat`) for system-output discovery and activity metering. PulseAudio-compatible `pactl` and `parec` are used when PipeWire discovery is unavailable.
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

Jimpachi lists system-output monitor sources, rather than microphone inputs. Use the arrow keys to highlight a source and `enter` to save it. If source discovery is unavailable, press `a` and enter the monitor source path shown by your audio server; Jimpachi stores that selection locally. It never installs audio dependencies or changes `PATH`.

## Planned storage

By default, Jimpachi will store its database and recordings beneath `~/.local/share/jimpachi/` and read configuration from `~/.config/jimpachi/config.toml`.
