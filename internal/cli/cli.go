// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package cli provides utilities for building command-line applications.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"

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

// HasFlags represents a command-line application that has flags.
type HasFlags interface {
	App

	// Flags adds flags to the flag set.
	Flags(*flag.FlagSet)
}

// AppFunc is a function type that implements the [App] interface.
// It has no defined flags.
type AppFunc func(context.Context, *Env) error

// Run calls f(ctx, env).
func (f AppFunc) Run(ctx context.Context, env *Env) error {
	return f(ctx, env)
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
	name := version.CmdName()

	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	if fa, ok := app.(HasFlags); ok {
		fa.Flags(flags)
	}

	var (
		cpuProfile = flags.String("cpuprofile", "", "Write CPU profile to `file`.")
		memProfile = flags.String("memprofile", "", "Write memory profile to `file`.")
	)
	var showVersion bool
	if flags.Lookup("version") == nil {
		flags.BoolVar(&showVersion, "version", false, "Show version.")
	}

	flags.Usage = usage(name, flags, env.Stderr)
	flags.SetOutput(env.Stderr)
	if err := flags.Parse(env.Args); err != nil {
		// Already printed to stderr by flag package, so mark as an unprintable error.
		return &unprintableError{err}
	}
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			return fmt.Errorf("could not create CPU profile: %w", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("could not start CPU profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if showVersion {
		fmt.Fprint(env.Stderr, version.Version())
		return ErrExitVersion
	}
	env.Args = flags.Args()

	if err := app.Run(ctx, env); err != nil {
		return err
	}

	if *memProfile != "" {
		f, err := os.Create(*memProfile)
		if err != nil {
			return fmt.Errorf("could not create memory profile: %w", err)
		}
		defer f.Close()
		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			return fmt.Errorf("could not write memory profile: %w", err)
		}
	}

	return nil
}

func usage(name string, flags *flag.FlagSet, stderr io.Writer) func() {
	return func() {
		if docSrc != nil {
			fmt.Fprintf(stderr, "%s\n", doc.Get(parseDocComment))
		}
		fmt.Fprint(stderr, "Available flags:\n\n")
		flags.PrintDefaults()
	}
}

var (
	docSrc []byte
	doc    syncx.Lazy[string]
)

// SetDocComment stores the provided byte slice as the source for the
// application's documentation comment.
//
// The parsing process assumes that the documentation comment is enclosed
// within a single /* ... */ block and extracts the content line by line.
// Any other multi-line comments within the embedded file will be ignored.
//
// The parsed documentation will be included in the help message.
//
// # Example usage
//
// In application's doc.go:
//
//	/*
//	Amazinator does amazing things...
//
//	# Usage
//
//		$ amazinator [flags...]
//
//	Amazinator amazes amazinations by amazing your amazinators.
//	*/
//	package main
//
//	import (
//		_ "embed"
//
//		"go.astrophena.name/tools/internal/cli"
//	)
//
//	//go:embed doc.go
//	var doc []byte
//
//	func init() { cli.SetDocComment(doc) }
func SetDocComment(src []byte) { docSrc = src }

func parseDocComment() string {
	s := bufio.NewScanner(bytes.NewReader(docSrc))
	var (
		doc       string
		inComment bool
	)
	for s.Scan() {
		line := s.Text()
		if line == "/*" {
			inComment = true
			continue
		}
		if line == "*/" {
			// Comment ended, stop scanning.
			break
		}
		if inComment {
			doc += s.Text() + "\n"
		}
	}
	if err := s.Err(); err != nil {
		panic(err)
	}
	return doc
}
