package devtools

//go:generate go run devtools/genhelpdoc.go
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
