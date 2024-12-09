// Copyright (c) 2021 Tailscale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file located at
// https://github.com/tailscale/tailscale/blob/main/LICENSE.

package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestDebugger(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()

	dbg1 := Debugger(mux)
	if dbg1 == nil {
		t.Fatal("didn't get a debugger from mux")
	}

	dbg2 := Debugger(mux)
	if dbg2 != dbg1 {
		t.Fatal("Debugger returned different debuggers for the same mux")
	}
}

func TestDebuggerKV(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	dbg := Debugger(mux)
	dbg.KV("Donuts", 42)
	dbg.KV("Secret code", "hunter2")
	val := "red"
	dbg.KVFunc("Condition", func() any { return val })

	body := getDebug(t, mux)
	for _, want := range []string{"Donuts", "42", "Secret code", "hunter2", "Condition", "red"} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q in output, not found", want)
		}
	}

	val = "green"
	body = getDebug(t, mux)
	for _, want := range []string{"Condition", "green"} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q in output, not found", want)
		}
	}
}

func TestDebuggerLink(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	dbg := Debugger(mux)
	dbg.Link("https://www.tailscale.com", "Homepage")

	body := getDebug(t, mux)
	for _, want := range []string{"https://www.tailscale.com", "Homepage"} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q in output, not found", want)
		}
	}
}

func TestDebuggerHandle(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	dbg := Debugger(mux)
	dbg.Handle("check", "Consistency check", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Test output")
	}))

	body := getDebug(t, mux)
	for _, want := range []string{"/debug/check", "Consistency check"} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q in output, not found", want)
		}
	}

	body = send(t, mux, http.MethodGet, "/debug/check", http.StatusOK)
	want := "Test output"
	if !strings.Contains(body, want) {
		t.Errorf("want %q in output, not found", want)
	}
}

func TestDebuggerGC(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	Debugger(mux)

	body := send(t, mux, http.MethodGet, "/debug/gc", http.StatusOK)
	testutil.AssertEqual(t, "Running GC...\nDone.\n", body)
}

func getDebug(t *testing.T, mux *http.ServeMux) string {
	return send(t, mux, http.MethodGet, "/debug/", http.StatusOK)
}
