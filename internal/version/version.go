// Package version provides the version and build information.
package version

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

// Info is the version and build information of the current binary.
type Info struct {
	Name    string `json:"name"` // name of the program
	Version string `json:"version"`
	Commit  string `json:"commit"`   // BuildInfo's vcs.revision
	BuiltAt string `json:"built_at"` // BuildInfo's vcs.date
	Go      string `json:"go"`       // runtime.Version()
	OS      string `json:"os"`       // runtime.GOOS
	Arch    string `json:"arch"`     // runtime.GOARCH
}

// String implements the fmt.Stringer interface.
func (i Info) String() string {
	var sb strings.Builder

	sb.WriteString(i.Short() + " (" + i.Go + ", " + i.OS + "/" + i.Arch + ")")
	if i.BuiltAt != "" {
		sb.WriteString("\n")
		sb.WriteString("built at " + i.BuiltAt)
	}
	sb.WriteString("\n")

	return sb.String()
}

// Short returns the short version information.
func (i Info) Short() string {
	ver := i.Version
	if ver == "devel" && i.Commit != "" {
		ver = i.Commit
	}
	return i.Name + "/" + ver
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

func initOnce() {
	i := &Info{
		Go:   runtime.Version(),
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		log.Printf("version: failed to read build information")
		return
	}

	i.Version = bi.Main.Version
	if i.Version == "(devel)" {
		i.Version = "devel"
	}

	i.Name = "cmd"
	exe, err := os.Executable()
	if err == nil {
		i.Name = filepath.Base(exe)
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			i.Commit = s.Value
		case "vcs.time":
			i.BuiltAt = s.Value
		}
	}
	info = *i
}
