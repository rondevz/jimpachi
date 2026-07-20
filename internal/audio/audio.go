// Package audio discovers system-output monitors and measures their activity.
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
}
