// Package geminiproxy implements a [http.Handler] that proxies requests to
// Gemini API. For now, it supports only [gemini.Client.GenerateContent] API
// method.
package geminiproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"go.astrophena.name/tools/internal/api/gemini"
	"go.astrophena.name/tools/internal/logger"
	"go.astrophena.name/tools/internal/web"
)

// New returns a new Proxy.
func New(client *gemini.Client, token string, logf logger.Logf) *Proxy {
	if logf == nil {
		logf = log.Printf
	}
	return &Proxy{client: client, token: token, logf: logf}
}

// Proxy is a [http.Handler] that proxies requests to Gemini API. For now, it
// supports only [gemini.Client.GenerateContent] API method.
type Proxy struct {
	client *gemini.Client
	token  string
	logf   logger.Logf
}

// ServeHTTP implements the [http.Handler] interface.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if token := r.Header.Get("X-Auth-Token"); token != p.token {
		web.RespondJSONError(p.logf, w, fmt.Errorf("%w: invalid token", web.ErrUnauthorized))
		return
	}

	rawParams, err := io.ReadAll(r.Body)
	if err != nil {
		web.RespondJSONError(p.logf, w, fmt.Errorf("%w: %v", web.ErrBadRequest, err))
		return
	}

	var params gemini.GenerateContentParams
	if err := json.Unmarshal(rawParams, &params); err != nil {
		web.RespondJSONError(p.logf, w, fmt.Errorf("%w: %v", web.ErrBadRequest, err))
		return
	}

	resp, err := p.client.GenerateContent(r.Context(), params)
	if err != nil {
		web.RespondJSONError(p.logf, w, fmt.Errorf("%w: %v", web.ErrInternalServerError, err))
		return
	}

	web.RespondJSON(w, resp)
}
