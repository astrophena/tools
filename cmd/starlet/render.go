// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
	"go.astrophena.name/base/web"
)

var errNoHost = errors.New("host hasn't set; pass it with -host flag or HOST environment variable")

func (e *engine) setWebhook(ctx context.Context) error {
	if e.host == "" {
		return errNoHost
	}
	u := &url.URL{
		Scheme: "https",
		Host:   e.host,
		Path:   "/telegram",
	}
	_, err := request.Make[any](ctx, request.Params{
		Method: http.MethodPost,
		URL:    "https://api.telegram.org/bot" + e.tgToken + "/setWebhook",
		Body: map[string]string{
			"url":          u.String(),
			"secret_token": e.tgSecret,
		},
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		HTTPClient: e.httpc,
		Scrubber:   e.scrubber,
	})
	return err
}

// selfPing continusly pings Starlet to prevent it's Render app from sleeping.
func (e *engine) selfPing(ctx context.Context, interval time.Duration) {
	env := cli.GetEnv(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			url := env.Getenv("RENDER_EXTERNAL_URL")
			if url == "" {
				e.logf("selfPing: RENDER_EXTERNAL_URL is not set; are you really on Render?")
				return
			}
			health, err := request.Make[web.HealthResponse](ctx, request.Params{
				Method: http.MethodGet,
				URL:    url + "/health",
				Headers: map[string]string{
					"User-Agent": version.UserAgent(),
				},
				HTTPClient: e.httpc,
				Scrubber:   e.scrubber,
			})
			if err != nil {
				e.logf("selfPing: %v", err)
			}
			if !health.OK {
				e.logf("selfPing: unhealthy: %+v", health)
			}
		case <-ctx.Done():
			return
		}
	}
}
