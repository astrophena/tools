// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"log"
	"os"
	"os/exec"

	"go.astrophena.name/tools/internal/devtools"
)

func main() {
	log.SetFlags(0)
	devtools.EnsureRoot()

	isCI := os.Getenv("CI") == "true"

	var w bytes.Buffer

	run(&w, "gofmt", "-d", ".")
	if diff := w.String(); diff != "" {
		log.Fatalf("Run gofmt on these files:\n\t%v", diff)
	}

	run(&w, "go", "tool", "staticcheck", "./...")

	if isCI {
		run(&w, "go", "test", "-race", "./...")
	} else {
		run(&w, "go", "test", "./...")
	}

	run(&w, "go", "mod", "tidy", "--diff")

	run(&w, "go", "tool", "addcopyright")
	run(&w, "go", "tool", "genreadme")
	// format Starlark files
	run(&w, "ruff", "format", "--no-cache", "--config", "extend-include = ['*.star']")
	if isCI {
		run(&w, "git", "diff", "--exit-code")
	}
}

func run(buf *bytes.Buffer, cmd string, args ...string) {
	buf.Reset()
	c := exec.Command(cmd, args...)
	c.Stdout = buf
	c.Stderr = buf
	if err := c.Run(); err != nil {
		log.Fatalf("%s failed: %v:\n%v", cmd, err, buf.String())
	}
}
