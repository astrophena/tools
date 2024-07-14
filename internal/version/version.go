// Package version provides the version and build information.
package version

import (
	"os"
	"path/filepath"
	"runtime"
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
	if ver == "devel" && i.Commit != "" {
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

var (
	once sync.Once
	info Info
)

// CmdName returns the base name of the current binary.
func CmdName() string {
	once.Do(initOnce)
	return info.Name
}

// Version returns the version and build information of the current binary.
func Version() Info {
	once.Do(initOnce)
	return info
}

func initOnce() { info = loadInfo(debug.ReadBuildInfo) }

func loadInfo(buildinfo func() (*debug.BuildInfo, bool)) Info {
	i := &Info{
		Go:   runtime.Version(),
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	bi, ok := buildinfo()
	if !ok {
		return *i
	}

	i.Version = bi.Main.Version
	if i.Version == "(devel)" {
		i.Version = "devel"
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

	return *i
}
