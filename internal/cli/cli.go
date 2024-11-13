// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package cli provides utilities for building command-line applications.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/util/syncx"
	"go.astrophena.name/tools/internal/version"
)

// Main is a helper function that handles common startup tasks for command-line
// applications. It sets up signal handling for interrupts, runs the application,
// and prints errors to stderr.
func Main(app App) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := Run(ctx, app, OSEnv())

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

// ErrExitVersion is an error indicating the application should exit after
// showing version.
var ErrExitVersion = &unprintableError{errors.New("version flag exit")}

// ErrInvalidArgs indicates that the command-line arguments provided to the
// application are invalid or insufficient.

// This error should be wrapped with fmt.Errorf to provide a specific,
// user-friendly message explaining the nature of the invalid arguments.
//
// For example:
//
//	return fmt.Errorf("%w: missing required argument 'filename'", cli.ErrInvalidArgs)
var ErrInvalidArgs = errors.New("invalid arguments")

// App represents a command-line application.
type App interface {
	// Run runs the application.
	Run(context.Context, *Env) error
}

// HasInfo represents a command-line application that has information about
// itself.
type HasInfo interface {
	App

	// Info returns information about the application.
	Info() Info
}

// HasFlags represents a command-line application that has flags.
type HasFlags interface {
	App

	// Flags adds flags to the flag set.
	Flags(*flag.FlagSet)
}

// AppFunc is a function type that implements the [App] interface.
// It has no information or flags.
type AppFunc func(context.Context, *Env) error

// Run calls f(ctx, env).
func (f AppFunc) Run(ctx context.Context, env *Env) error {
	return f(ctx, env)
}

// Info contains information about a command-line application.
type Info struct {
	Name        string
	Description string
}

// Env represents the application environment.
type Env struct {
	Args   []string
	Getenv func(string) string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	logf syncx.Lazy[logger.Logf]
}

// Logf writes the formatted message to standard error of this environment.
func (e *Env) Logf(format string, args ...any) {
	e.logf.Get(func() logger.Logf {
		return log.New(e.Stderr, "", 0).Printf
	})(format, args...)
}

// OSEnv returns the current operating system environment.
func OSEnv() *Env {
	return &Env{
		Args:   os.Args[1:],
		Getenv: os.Getenv,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Run handles the command-line application startup.
func Run(ctx context.Context, app App, env *Env) error {
	var info Info
	if ia, ok := app.(HasInfo); ok {
		info = ia.Info()
	} else {
		info = globalInfo
	}

	if info.Name == "" {
		info.Name = version.CmdName()
	}

	flags := flag.NewFlagSet(info.Name, flag.ContinueOnError)
	if fa, ok := app.(HasFlags); ok {
		fa.Flags(flags)
	}

	var showVersion bool
	if flags.Lookup("version") == nil {
		flags.BoolVar(&showVersion, "version", false, "Show version.")
	}

	flags.Usage = usage(info, flags, env.Stderr)
	flags.SetOutput(env.Stderr)
	if err := flags.Parse(env.Args); err != nil {
		// Already printed to stderr by flag package, so mark as an unprintable error.
		return &unprintableError{err}
	}
	if showVersion {
		fmt.Fprint(env.Stderr, version.Version())
		return ErrExitVersion
	}
	env.Args = flags.Args()

	return app.Run(ctx, env)
}

func usage(info Info, flags *flag.FlagSet, stderr io.Writer) func() {
	return func() {
		if info.Description != "" {
			fmt.Fprintf(stderr, "%s\n\n", strings.TrimSpace(info.Description))
		}
		fmt.Fprint(stderr, "Available flags:\n\n")
		flags.PrintDefaults()
	}
}

var globalInfo Info

// SetGlobalInfo sets the global application information.
//
// This function is used to inject application information, such as name and
// description, which is then used by the [Run] function if the application
// itself does not provide it via the [HasInfo] interface. This is primarily
// used for automatically generating documentation.
func SetInfo(info Info) { globalInfo = info }
