// Package cli contains common command-line flags and configuration
// options.
package cli

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync/atomic"

	"go.astrophena.name/tools/internal/version"
)

var started atomic.Bool

func ensureNotStarted() {
	if started.Load() {
		panic("cli.HandleStartup() was called previously")
	}
}

var opts struct {
	description, argsUsage string
}

// SetDescription sets the command description.
//
// Calling SetDescription after HandleStartup will panic.
func SetDescription(description string) {
	ensureNotStarted()
	opts.description = description
}

// SetArgsUsage sets the command arguments help string.
//
// Calling SetArgsUsage after HandleStartup will panic.
func SetArgsUsage(argsUsage string) {
	ensureNotStarted()
	opts.argsUsage = argsUsage
}

// HandleStartup handles the command startup.
//
// All flags should be defined before HandleStartup is called.
func HandleStartup() {
	started.Store(true)

	log.SetFlags(0)

	if opts.argsUsage == "" {
		opts.argsUsage = "[flags]"
	}
	flag.Usage = usage
	showVersion := flag.Bool("version", false, "Show version.")
	flag.Parse()

	if *showVersion {
		io.WriteString(os.Stderr, version.Version().String())
		os.Exit(0)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s %s\n\n", version.CmdName(), opts.argsUsage)
	if opts.description != "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", opts.description)
	}
	fmt.Fprint(os.Stderr, "Available flags:\n\n")
	flag.PrintDefaults()
}
