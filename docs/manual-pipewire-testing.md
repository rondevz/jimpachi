# Manual PipeWire Testing

Run this checklist from an active Ubuntu desktop session. The automated test suite uses fakes because the CI or automation shell cannot connect to the user's PipeWire session.

## Prerequisites

- PipeWire is running for the logged-in desktop user.
- `pw-dump` and `pw-cat` are available on `PATH`.
- A system-output monitor exists for the active speakers, headphones, or display output.

Start Jimpachi with an isolated data directory so the test does not alter normal local data:

```sh
XDG_DATA_HOME="$(mktemp -d)" go run .
```

## Source Picker

1. Confirm the recording-responsibility reminder appears on the first launch.
2. Confirm the Audio source list contains system-output monitor sources, not microphone inputs.
3. Play audible system audio, such as a browser video or test sound.
4. Move the highlight with `up`/`down` and verify only the highlighted source has an updating activity meter.
5. Confirm the source whose meter responds is the intended output monitor.
6. Press `enter` to confirm a source, quit with `q`, then start Jimpachi again with the same `XDG_DATA_HOME` value.
7. Confirm the selected source is restored and the responsibility reminder does not reappear.

## Recovery Paths

1. Start Jimpachi where PipeWire source discovery cannot connect or returns no monitor sources.
2. Confirm Jimpachi remains responsive and explains the discovery problem.
3. Press `a`, enter a known monitor-source path, then press `enter`.
4. Confirm the explicit source remains visible in the picker and receives an activity meter.
5. Confirm missing `pw-cat` or `parec` produces guidance identifying the missing activity-meter dependency.

## Expected Results

- Source discovery returns promptly when PipeWire is unavailable; Jimpachi must not appear hung.
- Selecting one source after another persists the most recently confirmed source.
- Source names render as ordinary text even if an audio server reports control characters.
- The meter is only an activity indicator. It does not identify applications, people, or the semantic source of audio.

## Not Covered Here

This checklist verifies source discovery, selection, persistence, and activity metering only. Actual system-output capture, Opus encoding, Recording creation, and Transcription are covered by later tickets.
