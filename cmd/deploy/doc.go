// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Deploy sends the service or site archive to the deployment server.

This tool is designed to be run within a GitHub Actions workflow. It
automates the process of authenticating with the deployment server
using an OIDC token and uploading the site archive for deployment.

# Usage

	$ go tool deploy <service or site> <archive>

Arguments:

  - service or site: The target service or site for deployment (e.g., "starlet" or "astrophena.name").
  - archive: The path to the service archive file (e.g., "archive.tar.gz").

# Environment Variables

This tool requires the following environment variables to be set by the
GitHub Actions runner:

  - ACTIONS_ID_TOKEN_REQUEST_URL: The URL to request the OIDC token from.
  - ACTIONS_ID_TOKEN_REQUEST_TOKEN: The bearer token for authenticating the
    OIDC token request.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
