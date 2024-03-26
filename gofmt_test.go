package tools

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestGofmt(t *testing.T) {
	var w bytes.Buffer

	gofmt := exec.Command("gofmt", "-d", ".")
	gofmt.Stdout = &w
	gofmt.Stderr = &w
	if err := gofmt.Run(); err != nil {
		t.Fatalf("gofmt failed: %v\n\n%v", err, w)
	}
}
