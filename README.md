# Jimpachi

Jimpachi is a terminal application for recording system audio, transcribing recordings locally, and optionally creating local AI summaries through Ollama.

## Prerequisites

- Go 1.24 or newer
- PipeWire (`pw-dump`, `pw-cat`) for system-output discovery and activity metering. PulseAudio-compatible `pactl` and `parec` are used when PipeWire discovery is unavailable.
- FFmpeg with `libopus` support for capture encoding.
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

Press `q` to quit. Press `r` to capture the selected source and `s` to stop. Stopping opens Recording detail; press `e` there to edit its title. Audio is stored under the Jimpachi data directory as mono 32 kbps Opus.

Recordings default to a 60-minute limit. Jimpachi warns five minutes before it stops only its own system-output capture; it never ends or changes the underlying call. Press `[` or `]` to adjust the limit in five-minute increments, or `l` to disable and re-enable it. The chosen limit is stored locally. If Jimpachi is interrupted, it recovers a decodable staged audio file on the next start and marks that Recording as interrupted.

Jimpachi lists system-output monitor sources, rather than microphone inputs. Use the arrow keys to highlight a source and `enter` to save it. If source discovery is unavailable, press `a` and enter the monitor source path shown by your audio server; Jimpachi stores that selection locally. It never installs audio dependencies or changes `PATH`.

## Storage

By default, Jimpachi stores its database and recordings beneath `~/.local/share/jimpachi/` and will read configuration from `~/.config/jimpachi/config.toml`.
