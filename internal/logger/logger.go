// Package logger defines a type for writing to logs. It's just a convenience
// type so that we don't have to pass verbose func(...) types around.
package logger

// Logf is the basic logger type: a printf-like func. Like log.Printf, the
// format need not end in a newline. Logf functions must be safe for concurrent
// use.
type Logf func(format string, args ...any)
