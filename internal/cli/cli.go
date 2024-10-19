// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package cli contains common command-line flags and configuration options.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"go.astrophena.name/tools/internal/version"
)

// Run executes the provided function f within a context that is canceled when
// an interrupt signal (e.g., Ctrl+C) is received.
//
// If f returns a non-nil error, Run prints the error message to standard error
// (if the error is considered "printable") and exits the program with a status
// code of 1.
//
// Printable errors are those that provide useful information to the user.
// Errors like flag parsing errors or help requests are considered non-printable
// and are not displayed to the user.
func Run(f func(ctx context.Context) error) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := f(ctx)

	if err == nil {
		return
	}

	if isPrintableError(err) {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(1)
}

type unprintableError struct{ err error }

func (e *unprintableError) Error() string { return e.err.Error() }
func (e *unprintableError) Unwrap() error { return e.err }

func isPrintableError(err error) bool {
	if errors.Is(err, flag.ErrHelp) {
		return false
	}
	var ue *unprintableError
	return !errors.As(err, &ue)
}

var (
	// ErrExitVersion is an error indicating the application should exit after
	// showing version.
	ErrExitVersion = &unprintableError{errors.New("version flag exit")}
	// ErrInvalidArgs indicates that the command-line arguments provided to the
	// application are invalid or insufficient. This error should be wrapped with
	// fmt.Errorf to provide a specific, user-friendly message explaining the
	// nature of the invalid arguments. For example:
	//
	// 	return fmt.Errorf("%w: missing required argument 'filename'", cli.ErrInvalidArgs)
	//
	ErrInvalidArgs = errors.New("invalid arguments")
)

// App represents a command-line application.
type App struct {
	Name        string        // Name of the application.
	Description string        // Description of the application.
	Credits     string        // Licenses of third-party libraries used in the application.
	ArgsUsage   string        // Usage message for the command-line arguments.
	Flags       *flag.FlagSet // Command-line flags.
}

// HandleStartup handles the command startup. All exported fields shouldn't be
// modified after HandleStartup is called.
//
// It sets up the command-line flags, parses the arguments, and handles the
// version flag if specified.
func (a *App) HandleStartup(args []string, stdout, stderr io.Writer) error {
	if a.Name == "" {
		a.Name = version.CmdName()
	}
	if a.ArgsUsage == "" {
		a.ArgsUsage = "[flags...]"
	}
	if a.Flags == nil {
		a.Flags = flag.NewFlagSet(a.Name, flag.ContinueOnError)
	}

	var showVersion bool
	if a.Flags.Lookup("version") == nil {
		a.Flags.BoolVar(&showVersion, "version", false, "Show version.")
	}
	var showCredits bool
	if a.Credits != "" && a.Flags.Lookup("credits") == nil {
		a.Flags.BoolVar(&showCredits, "credits", false, "Show third-party licenses.")
	}

	a.Flags.Usage = a.usage(stderr)
	a.Flags.SetOutput(stderr)
	if err := a.Flags.Parse(args); err != nil {
		// Already printed to stderr by flag package, so mark as an unprintable error.
		return &unprintableError{err}
	}
	if showVersion {
		fmt.Fprint(stderr, version.Version())
		return ErrExitVersion
	}
	if showCredits {
		fmt.Fprintf(stderr, a.Credits)
		return ErrExitVersion
	}

	return nil
}

// usage prints the usage message for the application.
func (a *App) usage(stderr io.Writer) func() {
	return func() {
		fmt.Fprintf(stderr, "Usage: %s %s\n\n", a.Name, a.ArgsUsage)
		if a.Description != "" {
			fmt.Fprintf(stderr, "%s\n\n", strings.TrimSpace(a.Description))
		}
		fmt.Fprint(stderr, "Available flags:\n\n")
		a.Flags.PrintDefaults()
	}
}
