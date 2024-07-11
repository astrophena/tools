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

// Default represents the default application configuration.
var Default = &App{
	Name:  version.CmdName(),
	Flags: Flags,
}

// Flags holds the command-line flags for the default application.
var Flags = flag.NewFlagSet(version.CmdName(), flag.ContinueOnError)

// Args returns the non-flag command-line arguments.
func Args() []string { return Default.Flags.Args() }

// SetDescription sets the description of the application.
func SetDescription(description string) { Default.Description = description }

// SetArgsUsage sets the usage message for the command-line arguments.
func SetArgsUsage(argsUsage string) { Default.ArgsUsage = argsUsage }

// HandleStartup initializes the application and processes command-line arguments.
func HandleStartup() {
	if err := Default.HandleStartup(os.Args[1:], os.Stdout, os.Stderr); errors.Is(err, ErrExitVersion) {
		os.Exit(0)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// App represents a command-line application.
type App struct {
	Name        string        // Name of the application.
	Description string        // Description of the application.
	ArgsUsage   string        // Usage message for the command-line arguments.
	Flags       *flag.FlagSet // Command-line flags.
}

// ErrExitVersion is an error indicating the application should exit after showing version.
var ErrExitVersion = errors.New("version flag exit")

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
