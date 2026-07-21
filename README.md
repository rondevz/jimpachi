# Jimpachi

Jimpachi is a local-first terminal notebook for preserving and reviewing instructions heard through system audio. It records the selected output monitor, creates a timestamped Transcription with `whisper.cpp`, and can generate an auxiliary Summary with a local Ollama model.

The Recording is always the source of truth. A Transcription is a derived review aid, and a Summary is an interpretation that should be checked against both the Transcription and audio.

## What It Does

- Captures system output without intentionally including microphone input.
- Stores durable mono Opus Recordings and metadata locally.
- Transcribes completed Recordings locally with automatic language detection.
- Generates narrative Summaries with agreements, suggestions, actions, deadlines, and open questions through a loopback-only Ollama endpoint.
- Preserves Recording history, processing state, editable titles, and retryable failures in SQLite.
- Prioritizes new Recordings over queued post-processing.
- Opens authoritative audio externally and deletes completed Recordings only after confirmation.

Jimpachi does not join calls, end calls, install dependencies, download models, or send Recordings and Transcriptions to remote services.

## Platform

Jimpachi currently targets Linux desktop sessions using PipeWire. It has been tested on Ubuntu with PipeWire, WirePlumber, and PipeWire's PulseAudio compatibility service.

## Requirements

### Required

- Go 1.24 or newer and a C toolchain to build Jimpachi (`go-sqlite3` uses CGO).
- PipeWire tools: `pw-dump` and `pw-cat`.
- PulseAudio-compatible tools: `pactl` and `parec`.
- FFmpeg with `libopus`, plus `ffprobe`.
- `xdg-open` to open saved audio from Jimpachi.
- An active system-output monitor source.

On Ubuntu:

```sh
sudo apt update
sudo apt install build-essential pipewire-bin pulseaudio-utils ffmpeg xdg-utils
```

Confirm that the active output exposes a monitor:

```sh
pactl list short sources
```

Select a source ending in `.monitor`, such as `bluez_output.<device>.monitor` or `alsa_output.<device>.monitor`. Entries beginning with `alsa_input` are microphones and should not be selected.

### Optional Local Processing

- [`whisper.cpp`](https://github.com/ggml-org/whisper.cpp) and a GGML model for Transcription.
- [Ollama](https://ollama.com/download/linux) and a text model for Summary generation.

## Install

Jimpachi does not yet publish release binaries. Build it from source:

```sh
git clone https://github.com/rondevz/jimpachi.git
cd jimpachi
go build -o jimpachi .
install -Dm755 jimpachi "$HOME/.local/bin/jimpachi"
```

Ensure `$HOME/.local/bin` is on `PATH`, then run:

```sh
jimpachi
```

For development, run directly from the repository:

```sh
go run .
```

Jimpachi uses the terminal's alternate screen and restores the previous shell contents when it exits.

## Configure Local Processing

Press `g` in Jimpachi to open Settings. Configure the following fields and press `s` to validate and save them.

### whisper.cpp

Install CMake, then build `whisper-cli` and download a model using the upstream quick start:

```sh
sudo apt install cmake
git clone https://github.com/ggml-org/whisper.cpp.git
cd whisper.cpp
sh ./models/download-ggml-model.sh small
cmake -B build
cmake --build build -j --config Release
```

Example Settings values:

```text
Whisper executable: /home/me/whisper.cpp/build/bin/whisper-cli
Whisper model:      /home/me/whisper.cpp/models/ggml-small.bin
CPU threads:        3
```

Jimpachi converts each completed Opus Recording to a temporary 16 kHz mono WAV for `whisper-cli`, then removes the temporary files.

### Ollama

Install and start Ollama using its official Linux instructions, then pull a text model:

```sh
ollama pull llama3.2:1b
```

Example Settings values:

```text
Ollama endpoint: http://127.0.0.1:11434
Ollama model:    llama3.2:1b
```

Jimpachi accepts only HTTP loopback endpoints and rejects redirects. Summary output is schema-constrained and is persisted only after the complete stream validates.

Configuration is stored at `~/.config/jimpachi/config.toml`, or beneath `$XDG_CONFIG_HOME/jimpachi/` when `XDG_CONFIG_HOME` is set.

## Use

The interface adapts to terminal width. Wide terminals show Library, Recording note, and Context columns. Narrow terminals switch to a focused view. The command bar displays actions available in the current state.

### Library And Capture

| Key | Action |
| --- | --- |
| `tab` | Move focus between Audio source and Library |
| `up`, `down` | Move within the focused list |
| `enter` | Confirm an Audio source or open a Recording |
| `r` | Start a Recording from the selected system-output monitor |
| `s` | Stop and save the active Recording |
| `g` | Open Settings |
| `q` | Quit when no text editor is active; an unfinished active Recording is discarded |

### Recording Detail

| Key | Action |
| --- | --- |
| `tab` | Cycle Summary, Transcript, and Recording sections |
| `up`, `down` | Scroll the active section |
| `o` | Open the saved audio with the desktop application |
| `e` | Edit the Recording title |
| `t` | Request or retry Transcription |
| `m` | Request or retry Summary generation |
| `c` | Cancel queued or running post-processing |
| `d` | Request deletion; `y` confirms and `n` cancels |
| `esc` | Return to the Library |

### Settings

| Key | Action |
| --- | --- |
| `a` | Toggle automatic Transcription |
| `l` | Enable or disable the Recording limit |
| `[`, `]` | Adjust the Recording limit by five minutes |
| `w` | Edit the `whisper-cli` executable path |
| `m` | Edit the Whisper model path |
| `t` | Edit the CPU thread cap |
| `o` | Edit the Ollama endpoint |
| `n` | Edit the Ollama model |
| `s` | Validate and save Settings |
| `up`, `down` | Scroll Settings on short terminals |
| `esc` | Close Settings or cancel the active editor |

Automatic Transcription is enabled by default. Each successful Transcription automatically queues a Summary; `m` requests or retries one manually. If Ollama is not configured or available, Summary generation fails safely while preserving the Recording and Transcription. The Recording limit defaults to 60 minutes and can be disabled. Starting a new Recording pauses queued work and cancels the active local processing attempt so it can safely resume afterward.

## Data And Privacy

By default, Jimpachi stores data beneath:

```text
~/.local/share/jimpachi/
├── jimpachi.db
└── recordings/
    └── <recording-id>.opus
```

Set `XDG_DATA_HOME` to change the data root. Recording IDs remain stable across history, detail, and processing state.

Privacy boundaries:

- Jimpachi selects system-output monitor sources, not microphone sources.
- PipeWire/PulseAudio may display a generic microphone privacy indicator for any monitor capture. This does not by itself mean microphone audio is included.
- Idle activity polling is disabled for PulseAudio-compatible monitors to avoid privacy-indicator flicker and extra capture streams.
- Whisper processing is local.
- Ollama requests are limited to loopback HTTP endpoints.
- Raw tool output, local paths, and spoken text are not copied into user-visible processing failures.
- Confirmed deletion of a completed Recording removes its audio, SQLite metadata, derived artifacts, and queued work.
- Quitting during capture discards the unfinished active Recording and its staging file.

Record system output only when everyone involved knows and agrees. You are responsible for complying with applicable laws and policies.

## Troubleshooting

### No System-Output Monitors

Install the PulseAudio-compatible client tools and inspect available sources:

```sh
sudo apt install pulseaudio-utils
pactl list short sources
```

Choose a `.monitor` entry. Do not use `alsa_input...`; it is a microphone. If no monitor appears, verify the user services:

```sh
systemctl --user status pipewire pipewire-pulse wireplumber
```

### GNOME Shows A Microphone Indicator

GNOME uses a generic recording indicator for microphone and output-monitor streams. Verify the selected source ends in `.monitor`. Jimpachi does not poll PulseAudio-compatible monitors while idle, but the indicator can remain visible during an actual Recording.

### Transcription Reports A Configuration Failure

Open Settings with `g` and verify that the executable is an executable file, the model is a readable regular file, and the CPU thread count is positive. FFmpeg must also be on `PATH` so Jimpachi can prepare the temporary WAV input.

### Summary Reports A Configuration Or Execution Failure

Confirm that Ollama is running and that the configured model is installed:

```sh
ollama list
```

The endpoint must use `http://localhost`, `http://127.0.0.1`, or another loopback address. Remote hosts and redirects are intentionally rejected.

## Develop

Run the complete verification gate:

```sh
go test ./...
go vet ./...
go build ./...
go test -race ./...
```

Manual PipeWire validation requires an active desktop audio session. See [`docs/manual-pipewire-testing.md`](docs/manual-pipewire-testing.md).

## License

Jimpachi is available under the [MIT License](LICENSE).
