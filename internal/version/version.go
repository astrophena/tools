// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package version provides the version and build information.
package version

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
)

// Info is the version and build information of the current binary.
type Info struct {
	Name    string `json:"name"`     // name of the program
	Version string `json:"version"`  // BuildInfo's module version
	Commit  string `json:"commit"`   // BuildInfo's vcs.revision
	BuiltAt string `json:"built_at"` // BuildInfo's vcs.date
	Dirty   bool   `json:"dirty"`    // BuildInfo's vcs.modified
	Go      string `json:"go"`       // runtime.Version()
	OS      string `json:"os"`       // runtime.GOOS
	Arch    string `json:"arch"`     // runtime.GOARCH
}

// String implements the fmt.Stringer interface.
func (i Info) String() string {
	var sb strings.Builder

	ver := i.Version
	if ver == "git" && i.Commit != "" {
		ver = "git-" + i.Commit
		if i.Dirty {
			ver += "-dirty"
		}
	}
	sb.WriteString(i.Name + " " + ver)
	if i.Commit != "" && !i.Dirty {
		sb.WriteString(" (https://github.com/astrophena/tools/commit/" + i.Commit + ")")
	}
	sb.WriteString("\n")

	sb.WriteString("built with " + i.Go + ", " + i.OS + "/" + i.Arch + "\n")
	if i.BuiltAt != "" {
		sb.WriteString("built at " + i.BuiltAt)
	}
	sb.WriteString("\n")

	return strings.TrimSpace(sb.String()) + "\n"
}

// CmdName returns the base name of the current binary.
func CmdName() string { return info().Name }

// Version returns the version and build information of the current binary.
func Version() Info { return info() }

// UserAgent returns a user agent string by combining the version information
// and a special URL leading to bot information page.
func UserAgent() string {
	i := Version()
	ver := i.Version
	if i.Version == "git" && i.Commit != "" {
		ver = i.Commit
	}
	return i.Name + "/" + ver + " (+https://astrophena.name/bleep-bloop)"
}

var (
	loadFunc = debug.ReadBuildInfo // used in tests

	info = sync.OnceValue(func() Info {
		return loadInfo(loadFunc)
	})
)

func loadInfo(buildinfo func() (*debug.BuildInfo, bool)) Info {
	bi, ok := buildinfo()
	if !ok {
		panic("build info is absent from binary; make sure you build it with module support")
	}

	i := &Info{Go: bi.GoVersion}

	i.Version = bi.Main.Version
	if i.Version == "(devel)" {
		i.Version = "git"
	}

	i.Name = strings.TrimPrefix(bi.Path, bi.Main.Path+"/cmd/")
	// Corner case for tests.
	if i.Name == "" {
		exe, err := os.Executable()
		if err == nil {
			i.Name = filepath.Base(exe)
		}
	}

	for _, s := range bi.Settings {
		switch s.Key {
		case "GOOS":
			i.OS = s.Value
		case "GOARCH":
			i.Arch = s.Value
		case "vcs.revision":
			i.Commit = s.Value
			if len(s.Value) >= 8 {
				i.Commit = s.Value[0:8]
			}
		case "vcs.modified":
			if s.Value == "true" {
				i.Dirty = true
			}
		case "vcs.time":
			i.BuiltAt = s.Value
		}
	}

	// If built without VCS info, fallback to "unknown".
	if i.Version == "git" && i.Commit == "" && i.BuiltAt == "" {
		i.Version = "unknown"
	}

	return *i
}
