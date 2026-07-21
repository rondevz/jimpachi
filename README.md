# Jimpachi

Jimpachi is a terminal application for recording system audio, transcribing recordings locally, and optionally creating local AI summaries through Ollama.

## Prerequisites

- Go 1.24 or newer
- PipeWire (`pw-dump`, `pw-cat`) for system-output discovery and activity metering. PulseAudio-compatible `pactl` and `parec` are used when PipeWire discovery is unavailable.
- FFmpeg with `libopus` support for capture encoding.
- `whisper.cpp` for local transcription

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

Press `tab` to focus Recording history, use `up`/`down` to select a prior Recording, and press `enter` to open its persisted detail and Transcription. Press `esc` in detail to return to history.

Recordings default to a 60-minute limit. Jimpachi warns five minutes before it stops only its own system-output capture; it never ends or changes the underlying call. Press `[` or `]` to adjust the limit in five-minute increments, or `l` to disable and re-enable it. The chosen limit is stored locally. If Jimpachi is interrupted, it recovers a decodable staged audio file on the next start and marks that Recording as interrupted.

Jimpachi lists system-output monitor sources, rather than microphone inputs. Use the arrow keys to highlight a source and `enter` to save it. If source discovery is unavailable, press `a` and enter the monitor source path shown by your audio server; Jimpachi stores that selection locally. It never installs audio dependencies or changes `PATH`.

## Transcription

Jimpachi runs configured `whisper.cpp` locally with language auto-detection and three CPU threads by default. It saves each timestamped segment in SQLite and displays the full document in Recording detail. Automatic transcription is enabled by default; press `p` to disable or re-enable it. Press `t` in a Recording detail view to request transcription manually.

Post-processing is serial and its queued, active, failed, or cancelled state is visible in Recording history and detail. Recording has priority: starting a new capture pauses queued work and cancels the active attempt so it can resume afterward. Press `c` in a queued or active Recording detail view to cancel it; press `t` to retry a failed or cancelled attempt. Failures show a stable, user-safe category and guidance while preserving the Recording and any previous Transcription.

After a successful Transcription, Jimpachi generates a local Summary with a proposed title, overview, agreements and decisions, action items, deadlines, and open questions when they are present. The proposed title applies only while the original timestamp title remains unchanged, so it cannot overwrite an edited title. Press `m` in Recording detail to generate or retry a Summary, or `c` while it is queued or running to cancel it. Ollama failures preserve the Recording and Transcription and show safe retry guidance.

Configure the executable and model paths in `~/.config/jimpachi/config.toml`, or under `$XDG_CONFIG_HOME/jimpachi/config.toml` when `XDG_CONFIG_HOME` is set:

```toml
[whisper]
executable = "/home/me/whisper.cpp/build/bin/whisper-cli"
model = "/home/me/whisper.cpp/models/ggml-small.bin"
# Optional; defaults to 3.
threads = 3

[ollama]
endpoint = "http://127.0.0.1:11434"
model = "llama3.2"
```

Jimpachi does not download models, install whisper.cpp, or modify `PATH`. If the paths are absent or invalid, manual transcription reports the local setup error and the Recording audio remains unchanged.

## Storage

By default, Jimpachi stores its database and recordings beneath `~/.local/share/jimpachi/` and reads configuration from `~/.config/jimpachi/config.toml`.
