// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
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
	_, err := request.Make[request.IgnoreResponse](ctx, request.Params{
		Method: http.MethodPost,
		URL:    tgAPI + "/bot" + e.tgToken + "/setWebhook",
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
