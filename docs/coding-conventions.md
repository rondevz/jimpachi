# Go Coding Conventions

These conventions keep Jimpachi small, local-first, and easy to extend. Follow them unless an ADR records a deliberate exception.

## Package Design

- Organize packages by capability, not technical layer. Do not introduce broad `models`, `services`, `utils`, or `repositories` packages.
- Keep a capability's behavior, state, and tests close together. Move code only when a real second caller establishes a reusable capability.
- Prefer deep modules: a small interface that hides substantial behavior. Callers should not need to know SQLite, FFmpeg, whisper.cpp, Ollama, or queue details to use a workflow.
- Keep package APIs narrow. Export only what another package needs.
- Prefer concrete types for internal collaboration. Do not introduce an interface merely because a type exists.

## Application Workflow

- The application workflow module is the primary seam between Bubble Tea and Jimpachi behavior.
- It owns Recording lifecycle, Recording history, post-processing coordination, state transitions, and user-visible failure outcomes.
- Bubble Tea is a thin UI adapter. It renders workflow state and turns keys and messages into user intents.
- Do not run SQL, invoke FFmpeg, whisper.cpp, Ollama, or the system opener directly from Bubble Tea views or update logic.
- Keep UI state focused on presentation concerns such as selection, focus, viewport position, and transient feedback.

## Interfaces and Adapters

- Introduce an interface only at a genuine external seam that varies by environment or must be faked in a behavior test.
- Typical seams are audio capture/discovery, persistence, local processing, time, file operations, and external file opening.
- Define an interface next to the consumer that needs it, not beside an implementation or in a central interfaces package.
- Keep interfaces operation-specific and small. Avoid generic transport or manager interfaces that force callers and fakes to branch on flags or request types.
- An adapter owns interaction with one external system. Keep subprocess parsing, SQL details, and platform-specific behavior inside the relevant adapter.

## Domain and Data

- Use the vocabulary in `CONTEXT.md`: Recording, Audio source, Transcription, Summary, Post-processing, Processing failure, and Recording history.
- Treat a Recording as the source of truth. Transcription and Summary are derived artifacts and must not overwrite or replace the audio.
- Keep SQLite mapping and XDG storage details behind the persistence adapter.
- Preserve stable Recording IDs across UI, persistence, diagnostics, and processing work.

## Errors and Concurrency

- Return errors with enough context to explain the failed operation and preserve the original error with `%w` when callers need to distinguish it.
- Convert external failures into the stable Processing failure categories the workflow exposes to the UI; retain raw diagnostic detail separately.
- The workflow owns post-processing queueing, cancellation, and priority. Do not create detached goroutines that outlive a Recording or cannot be cancelled.
- Pass `context.Context` as the first parameter to operations that can block, invoke a subprocess, or access an external system.
- Recording always has priority over post-processing.

## Tests

- Use the standard `testing` package and place tests in `*_test.go` files beside the package they exercise.
- Test behavior through the application workflow seam. Use fakes for external adapters and verify user-visible outcomes, not call order or private implementation details.
- Prefer real implementations inside the capability under test; fake only the external seam.
- Use table-driven tests only when multiple inputs verify the same behavior.
- Run `go test ./...`, `go vet ./...`, and `gofmt` on changed Go code before handoff.

## Go Style

- Let `gofmt` decide formatting. Keep names idiomatic and avoid unnecessary abbreviations.
- Prefer small, cohesive functions and types, but do not split a capability into shallow wrappers.
- Avoid package-level mutable state. Inject configuration and external dependencies at construction time.
- Add comments only for exported identifiers or non-obvious invariants; comments should explain why, not restate code.
