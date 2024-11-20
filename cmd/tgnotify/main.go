// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.astrophena.name/base/request"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/version"
)

func main() { cli.Main(cli.AppFunc(run)) }

func run(ctx context.Context, env *cli.Env) error {
	if len(env.Args) != 1 {
		return fmt.Errorf("%w: expected only one argument: 'message'", cli.ErrInvalidArgs)
	}

	token := env.Getenv("STARLET_NOTIFY_TOKEN")
	if token == "" {
		return errors.New("missing environment variable STARLET_NOTIFY_TOKEN")
	}

	body := struct {
		Message string `json:"message"`
	}{
		Message: env.Args[0],
	}

	_, err := request.Make[any](ctx, request.Params{
		URL:    "https://bot.astrophena.name/notify",
		Method: http.MethodPost,
		Body:   body,
		Headers: map[string]string{
			"User-Agent":    version.UserAgent(),
			"Authorization": "Bearer " + token,
		},
	})
	return err
}
