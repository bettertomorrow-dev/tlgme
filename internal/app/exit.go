package app

import (
	"context"
	"errors"
	"fmt"
)

const (
	exitOK            = 0
	exitUnexpected    = 1
	exitInvalidInput  = 2
	exitMissingConfig = 3
	exitPromptTimeout = 4
	exitExternal      = 5
	exitInterrupted   = 130
)

// exitError carries the public process result without changing Run's API.
// quiet errors have already explained themselves, such as the setup wizard.
type exitError struct {
	code  int
	err   error
	quiet bool
}

func (e exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exited with code %d", e.code)
	}
	return e.err.Error()
}

func (e exitError) Unwrap() error { return e.err }

func withExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return exitError{code: code, err: err}
}

func quietExit(code int) error {
	return exitError{code: code, quiet: true}
}

func inputError(err error) error { return withExitCode(exitInvalidInput, err) }

func externalError(err error) error { return withExitCode(exitExternal, err) }

func exitResult(err error) (code int, quiet bool) {
	if err == nil {
		return exitOK, false
	}
	// Signal.NotifyContext and callers cancel their contexts with
	// context.Canceled. It takes priority even when an operation wrapped it.
	if errors.Is(err, context.Canceled) {
		return exitInterrupted, true
	}
	if errors.Is(err, errPromptTimeout) {
		return exitPromptTimeout, false
	}
	var classified exitError
	if errors.As(err, &classified) {
		return classified.code, classified.quiet
	}
	return exitUnexpected, false
}
