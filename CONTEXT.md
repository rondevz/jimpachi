# Jimpachi

Jimpachi is a local-first terminal application for preserving and reviewing instructions received through system audio.

## Language

**Recording**:
The persisted record of a user-controlled system-output capture, including a stable identifier, audio, start time, duration, and editable title. A recording does not include microphone input in v1.
_Avoid_: Call recording, meeting recording

**Recording ID**:
The stable identifier assigned to a recording when it is saved. It lets a user correlate the detail view with its SQLite row and diagnostic records.
_Avoid_: File name, title

**System output**:
The audio heard through the selected operating-system output monitor, including remote call participants and other audible applications.
_Avoid_: Call audio, speaker audio

**Audio source**:
A selectable operating-system output monitor from which a recording is captured. Jimpachi presents sources with a live audio-activity meter so the user can confirm the intended source before recording.
_Avoid_: Noise source, call source

**Transcription**:
Text produced locally from a completed recording. It is created automatically after recording by default, but can also be requested or disabled by the user. It is a review aid, not a live-call feature.
_Avoid_: Live transcription, captioning

**Summary**:
A locally generated quick view of a transcription. A summary is created automatically after transcription and is never evidence of what was said.
_Avoid_: Resume, notes

**Post-processing**:
The configured local transcription and summary work started after a recording stops. It can run automatically or be requested manually, without blocking recording or history access. Completed recordings are processed one at a time and wait when a new recording has priority.
_Avoid_: Live processing, call analysis

**Processing failure**:
A failed transcription or summary attempt that leaves its completed input and prior successful artifacts available. It records a clear cause and can be retried by the user.
_Avoid_: Lost recording, corrupted session

**Recording history**:
The durable collection of recordings retained until the user explicitly deletes them. It is the place for revisiting a recording, its transcription, summary, and processing status.
_Avoid_: Recent recordings, temporary list

**Recording limit**:
The configurable maximum duration of a recording. It defaults to 60 minutes, warns before stopping capture, and can be disabled by the user.
_Avoid_: Call limit, forced call end

## Evidence

**Source of truth**:
The recording is the authoritative account of a session. The transcription and summary are derived artifacts that may contain errors or omissions.
