package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"jimpachi/internal/app"
	"jimpachi/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fail(err)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dataDir, err := app.DataDir()
	if err != nil {
		return err
	}

	workflow, err := app.Open(ctx, dataDir)
	if err != nil {
		return err
	}

	p := tea.NewProgram(tui.New(ctx, workflow))
	_, runErr := p.Run()
	cancel()
	closeErr := workflow.Close()
	if runErr != nil {
		return fmt.Errorf("run terminal program: %w", errors.Join(runErr, closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("close application workflow: %w", closeErr)
	}

	return nil
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "Jimpachi failed: %v\n", err)
	os.Exit(1)
}
