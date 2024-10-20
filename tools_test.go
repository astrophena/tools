// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package devtools

//go:generate go run devtools/gencredits.go
//go:generate go run devtools/genhelpdoc.go
//go:generate go run devtools/gencopyright.go
//go:generate go run devtools/genreadme.go

import (
	"bytes"
	"os"
	"os/exec"
	"testing"
)

func TestGenerate(t *testing.T) {
	if os.Getenv("CI") != "true" {
		t.Skip("this test is only run in CI")
	}
	var w bytes.Buffer
	run(t, &w, "go", "generate")
	run(t, &w, "git", "diff", "--exit-code")
}

func TestGofmt(t *testing.T) {
	var w bytes.Buffer
	run(t, &w, "gofmt", "-d", ".")
	if diff := w.String(); diff != "" {
		t.Fatalf("run gofmt on these files:\n\t%v", diff)
	}
}

func run(t *testing.T, buf *bytes.Buffer, cmd string, args ...string) {
	buf.Reset()
	c := exec.Command(cmd, args...)
	c.Stdout = buf
	c.Stderr = buf
	if err := c.Run(); err != nil {
		t.Fatalf("%s failed: %v:\n%v", cmd, err, buf.String())
	}
}
