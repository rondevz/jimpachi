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

## Capture

1. With audible system audio playing and the intended source confirmed, press `r`.
2. Confirm the focused `RECORDING` state shows increasing elapsed duration and an updating source activity meter.
3. Press `s` and confirm the Recording detail displays its ID, start time, duration, and `.opus` audio path.
4. Verify the resulting file plays in a desktop audio player and is mono Opus at 32 kbps, for example: `ffprobe -v error -show_entries stream=codec_name,channels,bit_rate -of default=noprint_wrappers=1 <audio-path>`.
5. Press `e`, change the title, press `enter`, quit, and relaunch with the same data directory. Confirm the title remains changed in Recording history.
6. Start another capture and quit with `q`; confirm neither `pw-cat`/`parec` nor `ffmpeg` remains running afterward.

## Recording Limit And Recovery

1. Confirm the initial screen shows a 60-minute Recording limit. Press `[` and `]` to adjust it, quit, and restart with the same data directory; confirm the selected limit remains.
2. Set a short limit suitable for manual testing and start a capture with audible system audio. Confirm Jimpachi warns five minutes before the limit and stops only its capture at the limit while the call or other system audio continues.
3. Press `l` to disable the limit, start a capture past the prior limit, and confirm it remains active until `s` is pressed. Press `l` again to restore the 60-minute limit.
4. Start a capture with audible audio, forcefully terminate Jimpachi without using `q`, then restart it with the same data directory. Confirm a decodable `.partial.opus` file is recovered into history and visibly marked as an interrupted capture.

## Expected Results

- Source discovery returns promptly when PipeWire is unavailable; Jimpachi must not appear hung.
- Selecting one source after another persists the most recently confirmed source.
- Source names render as ordinary text even if an audio server reports control characters.
- The meter is only an activity indicator. It does not identify applications, people, or the semantic source of audio.

## Local Transcription

1. Configure valid local whisper.cpp executable and model paths as documented in the README.
2. Capture a short Recording, stop it, and wait in its detail view. Confirm a full `Transcription` document appears with timestamped segments and the detected spoken language is transcribed without selecting a language.
3. Press `p` to disable automatic transcription, capture another short Recording, and confirm it initially shows `No Transcription yet.`
4. Press `t` in that Recording detail view and confirm the full timestamped document appears.
5. Quit and relaunch using the same data directory. Confirm the Transcription remains visible in the Recording detail view.
6. Temporarily configure an invalid model path, manually request transcription, and confirm the Recording remains available and the setup error is shown.

## Not Covered Here

This checklist does not cover Summary generation.
