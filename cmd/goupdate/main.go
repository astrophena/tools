// goupdate checks the Go version specified in the go.mod file of a Go project,
// updates it to the latest Go version if it is outdated, and creates a GitHub
// pull request with the updated go.mod file.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"go.astrophena.name/tools/internal/cli"

	"golang.org/x/mod/modfile"
)

func main() {
	flag.Func("C", "Change to `dir` at startup.", os.Chdir)
	cli.HandleStartup()

	// Read go.mod and obtain it's Go version.
	b, err := os.ReadFile("go.mod")
	if err != nil {
		log.Fatalf("Failed to read go.mod: %v", err)
	}
	modFile, err := modfile.Parse("go.mod", b, nil)
	if err != nil {
		log.Fatalf("Failed to parse go.mod: %v", err)
	}
	modGoVersion := modFile.Go.Version

	// Obtain current Go version and check if update is needed.
	curGoVersion, err := getCurGoVersion()
	if err != nil {
		log.Fatalf("Failed to obtain current Go version: %v", err)
	}
	if modGoVersion == curGoVersion {
		log.Printf("Module and current Go versions are equal. Exiting.")
		os.Exit(0)
	}

	// Update Go version in go.mod.
	modFile.AddGoStmt(curGoVersion)
	ub, err := modFile.Format()
	if err != nil {
		log.Fatalf("Failed to format updated go.mod: %v", err)
	}
	if err := os.WriteFile("go.mod", ub, 0o644); err != nil {
		log.Fatalf("Failed to write updated go.mod: %v", err)
	}

	// Create a pull request.
	branch := "go-update-" + curGoVersion
	run("git", "config", "user.name", "github-actions[bot]")
	run("git", "config", "user.email", "41898282+github-actions[bot]@users.noreply.github.com")
	run("git", "checkout", "-b", branch)
	run("git", "add", "go.mod")
	run("git", "commit", "-m", "go.mod: update to "+curGoVersion)
	run("git", "push", "origin", branch)
	run("gh", "pr", "create", "-f")
}

// getCurGoVersion fetches the latest Go version from the Go downloads page and returns it.
func getCurGoVersion() (version string, err error) {
	res, err := http.Get("https://go.dev/dl/?mode=json")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	var versions []struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &versions); err != nil {
		return "", err
	}

	if len(versions) == 0 {
		return "", errors.New("no versions provided")
	}

	return strings.TrimPrefix(versions[0].Version, "go"), nil
}

// run executes a shell command and logs a fatal error if the command fails.
func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("Command %q failed: %v", name+" "+strings.Join(args, " "), err)
	}
}
