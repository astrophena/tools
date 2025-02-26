// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build ignore

// readme.go generates README.md from the documentation of the tools.

package main

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"strings"
)

func main() {
	var sb strings.Builder

	sb.WriteString("<!-- Generated by devtools/gen/readme.go; DO NOT EDIT. -->\n\n")
	sb.WriteString("This repository holds personal tools:\n\n")

	const template = `{{ if eq .Name "main" }}- {{ .Doc }}{{ end }}`
	var buf bytes.Buffer
	cmd := exec.Command("go", "list", "-f", template, "./cmd/...")
	cmd.Env = append(os.Environ(), "GOOS=linux")
	cmd.Stdout = &sb
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		log.Fatalf("'go list' failed with %v:\n%s", err, buf.String())
	}

	sb.WriteString("\nInstall them:\n\n")
	sb.WriteString("```sh\n")
	sb.WriteString("$ go install go.astrophena.name/tools/cmd/...@master\n")
	sb.WriteString("```\n")
	sb.WriteString("\n")
	sb.WriteString("**Be warned**: these tools are for personal use, subject to change without notice and may gain or lose functionality at any time.\n\n")
	sb.WriteString("The code here is... so-so.\n\n")
	sb.WriteString("See documentation at https://go.astrophena.name/tools.\n")

	if err := os.WriteFile("README.md", []byte(sb.String()), 0o644); err != nil {
		log.Fatalf("failed to write README.md: %v", err)
	}
}
