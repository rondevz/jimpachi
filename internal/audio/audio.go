// Package audio discovers and captures system-output monitors.
package audio

import "context"

// Source is an operating-system output monitor that can be recorded.
type Source struct {
	ID       string
	Name     string
	Explicit bool
}

// Adapter interacts with the operating system's audio server.
type Adapter interface {
	Sources(context.Context) ([]Source, error)
	Activity(context.Context, Source) (float64, error)
	Start(context.Context, Source, string) (Capture, error)
}

// Capture is a running system-output capture that can be stopped cleanly.
type Capture interface {
	Stop(context.Context) error
	Wait() error
}
