package web

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"

	"go.astrophena.name/tools/internal/logger"
	"go.astrophena.name/tools/internal/version"
)

// Conns returns an http.Handler that displays the list of active
// HTTP connections and associates it with the provided http.Server.
func Conns(logf logger.Logf, s *http.Server) http.Handler {
	ch := &connsHandler{logf: logf, conns: make(map[string]*conn)}
	s.ConnState = ch.connState
	return ch
}

// connsHandler is an http.Handler that displays the list of active connections.
// It's inspired by https://twitter.com/bradfitz/status/1349825913136017415.
type connsHandler struct {
	mu    sync.Mutex
	conns map[string]*conn

	logf logger.Logf

	tpl     *template.Template
	tplInit sync.Once
	tplErr  error
}

// conn represents an active HTTP connection.
type conn struct {
	Net   string
	Addr  string
	Time  time.Time
	State http.ConnState
}

// connState implements the http.Server.ConnState callback function.
func (ch *connsHandler) connState(c net.Conn, state http.ConnState) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	addr := c.RemoteAddr().String()
	if state == http.StateClosed {
		delete(ch.conns, addr)
		return
	}
	ac, ok := ch.conns[addr]
	if !ok {
		ch.conns[addr] = &conn{
			Net:  c.RemoteAddr().Network(),
			Addr: addr,
			Time: time.Now(),
		}
		ac = ch.conns[addr]
	}
	if ac.State != state {
		ac.State = state
	}
}

// ServeHTTP implements the http.Handler.
func (ch *connsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if r.FormValue("format") == "json" {
		j, err := json.Marshal(ch.conns)
		if err != nil {
			Error(ch.logf, w, r, fmt.Errorf("web.connsHandler: failed to marshal json: %v", err))
			return
		}
		w.Write(j)
		return
	}

	ch.tplInit.Do(ch.doTplInit)
	if ch.tplErr != nil {
		Error(ch.logf, w, r, fmt.Errorf("web.connsHandler: failed to initialize template: %w", ch.tplErr))
		return
	}

	var buf bytes.Buffer
	if err := ch.tpl.Execute(&buf, nil); err != nil {
		Error(ch.logf, w, r, err)
		return
	}
	buf.WriteTo(w)
}

//go:embed conns.html
var connsTmpl string

func (ch *connsHandler) doTplInit() {
	ch.tpl, ch.tplErr = template.New("conns").Funcs(template.FuncMap{
		"cmdName": func() string {
			return version.CmdName()
		},
		"conns": func() map[string]*conn {
			return ch.conns
		},
		"connCount": func() string {
			w := "connection"
			if len(ch.conns) > 1 {
				w += "s"
			}

			var idle int64
			for _, c := range ch.conns {
				if c.State == http.StateIdle {
					idle++
				}
			}

			return fmt.Sprintf("%d %s, %d idle.", len(ch.conns), w, idle)
		},
		"since": func(t time.Time) time.Duration {
			return time.Since(t)
		},
	}).Parse(connsTmpl)
}
