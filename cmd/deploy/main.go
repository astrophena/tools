// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/request"
)

func main() { cli.Main(new(app)) }

type tokenResponse struct {
	Value string `json:"value"`
}

const tokenAudience = "astrophena.name"

type app struct {
	// configuration
	typ string // service or site
}

func (a *app) Flags(fs *flag.FlagSet) {
	fs.StringVar(&a.typ, "type", "site", "Whether to deploy `site or service`.")
}

func (a *app) Run(ctx context.Context) error {
	if a.typ != "site" && a.typ != "service" {
		return fmt.Errorf("%w: invalid type, want site or service, got %q", cli.ErrInvalidArgs, a.typ)
	}

	env := cli.GetEnv(ctx)
	if len(env.Args) != 2 {
		return fmt.Errorf("%w: want service or host and archive path", cli.ErrInvalidArgs)
	}
	target, archive := env.Args[0], env.Args[1]

	requestURL := env.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	requestToken := env.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if requestURL == "" || requestToken == "" {
		return errors.New("ACTIONS_ID_TOKEN_REQUEST_URL and ACTIONS_ID_TOKEN_REQUEST_TOKEN should be set")
	}

	tokenResp, err := request.Make[tokenResponse](ctx, request.Params{
		Method: http.MethodGet,
		URL:    requestURL + "&audience=" + tokenAudience,
		Headers: map[string]string{
			"Authorization": "Bearer " + requestToken,
			"User-Agent":    "actions/oidc-client",
		},
	})
	if err != nil {
		return err
	}
	token := tokenResp.Value

	b, err := os.ReadFile(archive)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("archive", filepath.Base(archive))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, bytes.NewReader(b)); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://deploy.astrophena.name/"+a.typ+"/"+target, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := request.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	b, err = io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("wanted 200, got %d: %s", res.StatusCode, b)
	}

	return nil
}
