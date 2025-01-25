// Copyright (c) 2021 Tailscale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file located at
// https://github.com/tailscale/tailscale/blob/main/LICENSE.

// Adapted from https://pkg.go.dev/tailscale.com/tsweb#Debugger.

package web

import (
	"bytes"
	"cmp"
	_ "embed"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/version"
)

//go:embed templates/debug.html
var debugTemplate string

// DebugHandler is an [http.Handler] that serves a debugging "homepage", and
// provides helpers to register more debug endpoints and reports.
//
// The rendered page consists of three sections: header menu, informational
// key/value pairs and links to other pages.
//
// Callers can add to these sections using the MenuFunc, KV and Link helpers
// respectively.
//
// Additionally, the Handle method offers a shorthand for correctly registering
// debug handlers and cross-linking them from /debug/.
//
// Methods of DebugHandler can be safely called by multiple goroutines.
type DebugHandler struct {
	mux      *http.ServeMux                 // where this handler is registered
	mu       sync.RWMutex                   // covers all fields below, mux is protected by it's own mutex
	kvfuncs  []kvfunc                       // output one table row each, see KV()
	links    []link                         // one link in header
	menuFunc func(*http.Request) []MenuItem // function to generate the menu
	tpl      syncx.Lazy[*template.Template] // template that is used for rendering debug page
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

// MenuItem is a debug page header menu item.
type MenuItem interface {
	ToHTML() template.HTML
}

// HTMLItem is a [MenuItem] that can contain arbitrary HTML.
type HTMLItem string

func (hi HTMLItem) ToHTML() template.HTML { return template.HTML(hi) }

// LinkItem is a [MenuItem] that is a link.
type LinkItem struct {
	Name   string
	Target string
}

func (li LinkItem) ToHTML() template.HTML {
	return template.HTML("<a href=" + li.Target + ">" + html.EscapeString(li.Name) + "</a>")
}

// Debugger returns the [DebugHandler] registered on mux at /debug/, creating it
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

// ServeHTTP implements the [http.Handler] interface.
func (d *DebugHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/debug/" {
		// Sub-handlers are handled by the parent mux directly.
		RespondError(w, r, ErrNotFound)
		return
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	tpl, err := d.tpl.GetErr(func() (*template.Template, error) {
		return template.New("debug").Parse(debugTemplate)
	})
	if err != nil {
		RespondError(w, r, fmt.Errorf("failed to initialize template: %w", err))
		return
	}

	var menuItems []MenuItem
	if d.menuFunc != nil {
		menuItems = d.menuFunc(r)
	}

	var kvs []kv
	for _, kvf := range d.kvfuncs {
		kvs = append(kvs, kv{kvf.k, kvf.v()})
	}

	data := struct {
		CmdName    string
		Version    version.Info
		KVs        []kv
		HasIcon    bool
		Links      []link
		MenuItems  []MenuItem
		Stylesheet string
	}{
		CmdName:    version.CmdName(),
		Version:    version.Version(),
		KVs:        kvs,
		Links:      d.links,
		MenuItems:  menuItems,
		Stylesheet: StaticFS.HashName("static/css/main.css"),
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, &data); err != nil {
		RespondError(w, r, err)
		return
	}
	buf.WriteTo(w)
}

// Handle registers handler at /debug/<slug> and creates a descriptive entry in
// /debug/ for it.
func (d *DebugHandler) Handle(slug, desc string, handler http.Handler) {
	href := "/debug/" + slug
	d.mux.Handle(href, handler)
	d.Link(href, desc)
}

// HandleFunc is like Handle, but accepts [http.HandlerFunc] instead of
// [http.Handler].
func (d *DebugHandler) HandleFunc(slug, desc string, handler http.HandlerFunc) {
	d.Handle(slug, desc, http.HandlerFunc(handler))
}

// KV adds a key/value list item to /debug/.
func (d *DebugHandler) KV(k string, v any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.kvfuncs = append(d.kvfuncs, kvfunc{k, func() any {
		return v
	}})
}

// KVFunc adds a key/value list item to /debug/. v is called on every render of
// /debug/.
func (d *DebugHandler) KVFunc(k string, v func() any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.kvfuncs = append(d.kvfuncs, kvfunc{k, v})
}

// Link adds a URL and description list item to /debug/.
func (d *DebugHandler) Link(url, desc string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.links = append(d.links, link{url, desc})
	slices.SortStableFunc(d.links, func(a, b link) int {
		return cmp.Compare(a.Desc, b.Desc)
	})
}

// MenuFunc sets a function that generates custom menu items for /debug/ page
// header.
func (d *DebugHandler) MenuFunc(f func(*http.Request) []MenuItem) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.menuFunc = f
}
