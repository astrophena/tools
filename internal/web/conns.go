package web

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"

	"go.astrophena.name/tools/internal/logger"
	"go.astrophena.name/tools/internal/version"
)

// Conns returns an [http.Handler] that displays the list of active
// HTTP connections and associates it with the provided http.Server.
func Conns(logf logger.Logf, s *http.Server) http.Handler {
	ch := &connsHandler{logf: logf, conns: make(ConnMap)}
	s.ConnState = ch.connState
	return ch
}

// ConnMap represents active connections to the HTTP server.
type ConnMap map[string]*Conn

// Conn represents an active HTTP connection.
type Conn struct {
	Network string
	Addr    string
	Time    time.Time
	State   http.ConnState
}

// connsHandler is a [http.Handler] that displays the list of active connections.
// It's inspired by https://x.com/bradfitz/status/1349825913136017415.
type connsHandler struct {
	mu    sync.Mutex
	conns ConnMap

	logf logger.Logf

	tpl     *template.Template
	tplInit sync.Once
	tplErr  error
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
		ch.conns[addr] = &Conn{
			Network: c.RemoteAddr().Network(),
			Addr:    addr,
			Time:    time.Now(),
		}
		ac = ch.conns[addr]
	}
	if ac.State != state {
		ac.State = state
	}
}

func (ch *connsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if r.FormValue("format") == "json" {
		RespondJSON(w, ch.conns)
		return
	}

	ch.tplInit.Do(ch.doTplInit)
	if ch.tplErr != nil {
		RespondError(ch.logf, w, fmt.Errorf("conns: failed to initialize template: %w", ch.tplErr))
		return
	}

	var buf bytes.Buffer
	if err := ch.tpl.Execute(&buf, nil); err != nil {
		RespondError(ch.logf, w, err)
		return
	}
	buf.WriteTo(w)
}

//go:embed templates/conns.html
var connsTemplate string

func (ch *connsHandler) doTplInit() {
	ch.tpl, ch.tplErr = template.New("conns").Funcs(template.FuncMap{
		"cmdName": func() string {
			return version.CmdName()
		},
		"conns": func() ConnMap {
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
	}).Parse(connsTemplate)
}
