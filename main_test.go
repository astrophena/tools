package tools

//go:generate bash genreadme.sh

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"go.astrophena.name/tools/internal/testutil"
)

func TestReadme(t *testing.T) {
	if os.Getenv("CI") != "true" {
		t.Skip("this test is only run in CI")
	}

	read := func() []byte {
		b, err := os.ReadFile("README.md")
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	got := read()
	if err := exec.Command("go", "generate").Run(); err != nil {
		t.Fatalf("go generate failed: %v", err)
	}
	want := read()

	testutil.AssertEqual(t, want, got)
}

func TestGofmt(t *testing.T) {
	var w bytes.Buffer

	gofmt := exec.Command("gofmt", "-d", ".")
	gofmt.Stdout = &w
	gofmt.Stderr = &w
	if err := gofmt.Run(); err != nil {
		t.Fatalf("gofmt failed: %v\n\n%v", err, w)
	}
}
