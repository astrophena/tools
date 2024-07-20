// Package cli contains common command-line flags and configuration options.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"go.astrophena.name/tools/internal/version"
)

// Run is a helper that wraps a call to function that implements main in a
// program, and if error returned is not nil, it prints the error message (if
// error is printable) and exits with code 1.
func Run(err error) {
	if err == nil {
		return
	}
	if isPrintableError(err) {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(1)
}

func isPrintableError(err error) bool {
	if errors.Is(err, flag.ErrHelp) {
		return false
	}
	if errors.Is(err, ErrArgsNeeded) {
		return false
	}
	return true
}

// App represents a command-line application.
type App struct {
	Name        string        // Name of the application.
	Description string        // Description of the application.
	ArgsUsage   string        // Usage message for the command-line arguments.
	Flags       *flag.FlagSet // Command-line flags.
}

var (
	// ErrExitVersion is an error indicating the application should exit after
	// showing version.
	ErrExitVersion = errors.New("version flag exit")
	// ErrArgsNeeded is an error indicating the application needed some additional
	// flags or arguments passed to continue.
	ErrArgsNeeded = errors.New("additional flags or arguments needed")
)

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

	a.Flags.Usage = a.usage(stderr)
	a.Flags.SetOutput(stderr)
	if err := a.Flags.Parse(args); err != nil {
		return err
	}
	if showVersion {
		fmt.Fprint(stderr, version.Version())
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
