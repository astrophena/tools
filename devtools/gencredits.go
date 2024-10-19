// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build ignore

// gencredits.go generates credits for all commands.

package main

import (
	"context"
	"debug/buildinfo"
	"fmt"
	"go/build"
	"go/format"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"go.astrophena.name/base/request"
)

func main() {
	log.SetFlags(0)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	tmpdir, err := os.MkdirTemp("", "devtools-gencredits")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	if err := filepath.WalkDir("./cmd", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			return nil
		}

		pkg, err := build.ImportDir(path, build.IgnoreVendor)
		if err != nil || len(pkg.GoFiles) == 0 || pkg.Name != "main" {
			return nil
		}

		bin := filepath.Join(tmpdir, filepath.Base(pkg.Dir))
		if err := exec.Command("go", "build", "-C", path, "-o", bin).Run(); err != nil {
			return err
		}
		info, err := buildinfo.ReadFile(bin)
		if err != nil {
			return err
		}
		if len(info.Deps) == 0 {
			return nil
		}

		var sb strings.Builder
		sb.WriteString("// Code generated by devtools/gencredits.go; DO NOT EDIT.\n\n")
		sb.WriteString("package main\n\n")
		fmt.Fprintf(&sb, "const credits = `%s utilizes the following third-party libraries:\n\n", filepath.Base(pkg.Dir))
		for _, mod := range info.Deps {
			if strings.Contains(mod.Path, "go.astrophena.name") {
				continue
			}
			licenses, err := getLicenses(ctx, mod.Path, mod.Version)
			if err != nil {
				return err
			}
			if licenses == "" {
				licenses = "(unknown license)"
			}
			fmt.Fprintf(&sb, "- %s: %s\n", mod.Path, licenses)
		}
		sb.WriteString("`\n\n")
		src, err := format.Source([]byte(sb.String()))
		if err != nil {
			return err
		}

		return os.WriteFile(filepath.Join(path, "credits.go"), src, 0o644)
	}); err != nil {
		log.Fatal(err)
	}
}

// https://docs.deps.dev/api/v3alpha/#getversion
type info struct {
	Licenses []string `json:"licenses"`
}

func getLicenses(ctx context.Context, path, version string) (licenses string, err error) {
	mod, err := request.Make[info](ctx, request.Params{
		Method: http.MethodGet,
		URL:    "https://api.deps.dev/v3alpha/systems/go/packages/" + url.PathEscape(path) + "/versions/" + version,
	})
	if err != nil {
		return "", err
	}
	return strings.Join(mod.Licenses, ", "), nil
}
