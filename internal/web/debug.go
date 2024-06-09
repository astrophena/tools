// Copyright (c) 2021 Tailscale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from https://pkg.go.dev/tailscale.com/tsweb#Debugger.

package web

import (
	"bytes"
	_ "embed"
	"expvar"
	"fmt"
	"html/template"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"go.astrophena.name/tools/internal/version"
)

var expvarOnce sync.Once

//go:embed debug.html
var debugTmpl string

// DebugHandler is an http.Handler that serves a debugging "homepage", and
// provides helpers to register more debug endpoints and reports.
//
// The rendered page consists of two sections: informational key/value pairs and
// links to other pages.
//
// Callers can add to these sections using the KV and URL helpers respectively.
//
// Additionally, the Handle method offers a shorthand for correctly registering
// debug handlers and cross-linking them from /debug/.
type DebugHandler struct {
	mux     *http.ServeMux     // where this handler is registered
	kvfuncs []kvfunc           // output one table row each, see KV()
	links   []link             // one link in header
	tpl     *template.Template // template that is used for rendering debug page
	tplInit sync.Once          // guards template initialization
	tplErr  error              // error that happened during template initialization
}

// Utility types used for rendering templates.
type (
	kvfunc struct {
		k string
		v func() any
	}
	kv struct {
		K string
		V any
	}
	link struct{ URL, Desc string }
)

// Debugger returns the DebugHandler registered on mux at /debug/, creating it
// if necessary.
func Debugger(mux *http.ServeMux) *DebugHandler {
	h, pat := mux.Handler(&http.Request{URL: &url.URL{Path: "/debug/"}})
	if d, ok := h.(*DebugHandler); ok && pat == "/debug/" {
		return d
	}
	ret := &DebugHandler{mux: mux}
	mux.Handle("/debug/", ret)

	hostname, err := os.Hostname()
	if err == nil {
		ret.KV("Machine", hostname)
	}
	ret.KVFunc("Uptime", uptime)
	ret.Handle("vars", "Vars", expvar.Handler())
	expvarOnce.Do(func() {
		expvar.Publish("process_start_time", expvar.Func(func() any { return timeStart }))
		expvar.Publish("uptime", expvar.Func(func() any { return uptime().(time.Duration).String() }))
		expvar.Publish("version", expvar.Func(func() any { return version.Version() }))
		expvar.Publish("goroutines", expvar.Func(func() any { return runtime.NumGoroutine() }))
	})
	ret.Handle("pprof/", "pprof", http.HandlerFunc(pprof.Index))
	ret.Link("/debug/pprof/goroutine?debug=1", "Goroutines (collapsed)")
	ret.Link("/debug/pprof/goroutine?debug=2", "Goroutines (full)")
	ret.Handle("gc", "Force GC", http.HandlerFunc(serveGC))
	// Register this one directly on mux, rather than using ret.URL/etc, as we
	// don't need another line of output on the index page. The /pprof/ index
	// already covers it.
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))

	return ret
}

func serveGC(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Running GC...\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	runtime.GC()
	w.Write([]byte("Done.\n"))
}

var timeStart = time.Now()

func uptime() any { return time.Since(timeStart).Round(time.Second) }

// ServeHTTP implements the http.Handler.
func (d *DebugHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/debug/" {
		// Sub-handlers are handled by the parent mux directly.
		NotFound(w, r)
		return
	}

	d.tplInit.Do(func() {
		d.tpl, d.tplErr = template.New("debug").Parse(debugTmpl)
	})
	if d.tplErr != nil {
		Error(w, r, fmt.Errorf("failed to initialize template: %w", d.tplErr))
		return
	}

	var kvs []kv
	for _, kvf := range d.kvfuncs {
		kvs = append(kvs, kv{kvf.k, kvf.v()})
	}

	data := struct {
		CmdName string
		Version version.Info
		KVs     []kv
		Links   []link
	}{
		CmdName: version.CmdName(),
		Version: version.Version(),
		KVs:     kvs,
		Links:   d.links,
	}

	var buf bytes.Buffer
	if err := d.tpl.Execute(&buf, &data); err != nil {
		Error(w, r, err)
		return
	}
	buf.WriteTo(w)
}

// Handle registers handler at /debug/<slug> and creates a descriptive
// entry in /debug/ for it.
func (d *DebugHandler) Handle(slug, desc string, handler http.Handler) {
	href := "/debug/" + slug
	d.mux.Handle(href, handler)
	d.Link(href, desc)
}

// KV adds a key/value list item to /debug/.
func (d *DebugHandler) KV(k string, v any) {
	d.kvfuncs = append(d.kvfuncs, kvfunc{k, func() any {
		return v
	}})
}

// KVFunc adds a key/value list item to /debug/. v is called on every
// render of /debug/.
func (d *DebugHandler) KVFunc(k string, v func() any) {
	d.kvfuncs = append(d.kvfuncs, kvfunc{k, v})
}

// Link adds a URL and description list item to /debug/.
func (d *DebugHandler) Link(url, desc string) {
	d.links = append(d.links, link{url, desc})
}