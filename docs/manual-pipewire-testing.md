# Manual Desktop Testing

Run this checklist from an active Linux desktop session. Automated tests use fakes because CI and automation shells cannot reliably connect to the user's PipeWire, desktop opener, or Ollama session.

## Prerequisites

- PipeWire and WirePlumber are running for the logged-in desktop user.
- `pw-dump` and `pw-cat` are available from `pipewire-bin`.
- `pactl` and `parec` are available from `pulseaudio-utils`.
- `ffmpeg` and `ffprobe` are available.
- A system-output monitor exists for the active speakers, headphones, or display output.

Confirm monitor availability:

```sh
pactl list short sources
```

Use a source ending in `.monitor`. Never use an `alsa_input...` microphone source for these tests.

Start Jimpachi with isolated data and configuration directories so testing does not alter normal local state:

```sh
test_root="$(mktemp -d)"
export XDG_DATA_HOME="$test_root/data"
export XDG_CONFIG_HOME="$test_root/config"
go run .
```

Keep these exports in the same shell for relaunch tests. After testing, exit Jimpachi and remove the retained directory with `rm -rf "$test_root"`.

## Terminal Experience

1. Confirm Jimpachi opens in the terminal's alternate screen and the prior shell prompt is hidden.
2. Confirm quitting with `q` restores the previous terminal contents.
3. At 124 columns or wider, confirm the Library, Recording note, and Context columns are visible and extend to the command bar.
4. Resize below 124 columns and confirm the interface switches to a focused layout without horizontal overflow.
5. Resize to a short terminal and confirm the focused Library item, active editor, errors, and command bar remain visible or reachable by scrolling.
6. Confirm the command bar changes with source focus, Library focus, active Recording, Settings, and Recording detail.

## Source Selection

1. Confirm the recording-responsibility reminder appears on the first launch.
2. Confirm the Audio source list contains system-output monitor sources, not microphone inputs.
3. Press `tab` to move focus between Audio source and Library; confirm the active focus marker and command bar agree.
4. Use `up`/`down` and `enter` to select the intended `.monitor` source.
5. Quit and relaunch from the same shell; confirm the selected source is restored and the reminder does not reappear.
6. Confirm source names render as ordinary text even if the audio server reports terminal control characters.

Native PipeWire monitors may display an activity meter. PulseAudio-compatible monitors intentionally do not poll while idle, because repeated `parec` probes trigger generic desktop recording indicators and can disrupt Bluetooth audio.

## Source Recovery

1. Start Jimpachi where source discovery cannot connect or returns no monitor sources.
2. Confirm Jimpachi remains responsive and explains the discovery problem.
3. Press `a`, enter a known `.monitor` source path, then press `enter`.
4. Confirm the explicit source remains visible after selection and restart.
5. Confirm invalid or stale non-monitor sources cannot start capture.
6. Confirm missing `pw-cat` and `parec` produce actionable dependency guidance.

## Recording

1. Play audible system audio and confirm the intended output monitor.
2. Press `r`; confirm the focused Recording page shows `RECORDING`, elapsed duration, source, and stop guidance.
3. Speak near the microphone while system audio plays. Press `s`, open the audio with `o`, and verify the Recording contains system output but not microphone input.
4. Confirm the Recording detail shows its stable ID, start time, duration, and `.opus` audio path under the Recording tab.
5. Verify the file is mono Opus, for example:

   ```sh
   ffprobe -v error -show_entries stream=codec_name,channels,bit_rate -of default=noprint_wrappers=1 <audio-path>
   ```

6. Press `e`, change the title, press `enter`, quit, and relaunch. Confirm the title remains changed in the Library.
7. Start another capture and quit with `q`; confirm the unfinished Recording is discarded and neither `pw-cat`/`parec` nor `ffmpeg` remains running afterward.

The desktop may show a generic microphone privacy indicator during output-monitor capture. Source routing and the resulting file, not the generic icon, determine whether microphone audio was included.

## Recording Limit And Recovery

1. Confirm the initial 60-minute Recording limit appears in Context or Settings.
2. Press `[` and `]` to adjust it, quit, and restart; confirm the value persists.
3. Set a short limit suitable for testing and start capture. Confirm Jimpachi warns before the limit and stops only its own Recording.
4. Press `l` to disable the limit, capture past the prior limit, and confirm capture remains active until `s` is pressed.
5. Start capture, forcefully terminate Jimpachi, then restart from the same shell. Confirm the decodable `.partial.opus` staging file is promoted to a final `.opus` Recording in Library and marked as interrupted.

## Local Transcription

1. Configure valid `whisper-cli`, GGML model, and CPU thread values in Settings.
2. Capture a short Recording and stop it.
3. Confirm Library transitions from queued to Transcribing and clears the yellow processing marker after completion.
4. Open Transcript and confirm timestamped segments appear with automatic language detection.
5. Disable automatic Transcription in Settings, capture another Recording, and confirm it initially has no Transcription.
6. Press `t` in detail and confirm the manual Transcription completes.
7. Quit and relaunch; confirm the Transcription remains available.
8. Temporarily configure an invalid model path and confirm validation prevents saving or processing fails safely without altering audio.

## Local Summary

1. Configure a loopback Ollama endpoint and installed text model in Settings.
2. Complete a Transcription and confirm Summary generation queues automatically.
3. Confirm generated-character progress updates while Ollama streams locally.
4. Confirm the final Summary contains a narrative overview and only relevant agreements, suggestions, actions, deadlines, and open questions.
5. Confirm the Summary identifies itself as auxiliary and directs the user to verify it against Transcription and audio.
6. Press `m` and confirm manual Summary regeneration completes.
7. Confirm the yellow Library processing marker clears after Summary completion or failure.
8. Stop Ollama, retry Summary with `m`, and confirm the Recording and Transcription remain available with safe retry guidance.

## Artifacts And Deletion

1. Open a Recording and cycle Summary, Transcript, and Recording with `tab`.
2. Scroll long content with `up`/`down` and confirm scrolling stops at the content boundary.
3. Press `o` and confirm the desktop opens the authoritative audio file.
4. Press `d`; confirm no deletion occurs before `y`.
5. Cancel with `n`, then repeat and confirm with `y`.
6. Confirm audio, SQLite metadata, Transcription, Summary, and queued work are removed from Library.

## Expected Results

- Source discovery returns promptly when PipeWire is unavailable.
- Only verified system-output monitor sources can start capture.
- Selecting a new source persists the latest confirmed choice.
- Pulse monitor capture uses `parec` and does not fall back to the default microphone.
- The Recording remains the authoritative artifact through every processing failure.
- Starting a Recording has priority over queued local processing.
- No dynamic source, title, path, configuration, Transcription, or Summary text can emit terminal control sequences.
