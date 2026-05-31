// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package golang

import (
	"context"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
	"go.starlark.net/starlark"
	modulepkg "golang.org/x/mod/module"
	"golang.org/x/sync/errgroup"
)

var goProxyURL = "https://proxy.golang.org"

// Module returns the Starlark go module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "go" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"install":       starlark.NewBuiltin("go.install", m.install),
		"install_local": starlark.NewBuiltin("go.install_local", m.installLocal),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) install(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		packages *starlark.List
		cwd      string
		ldflags  string = "-s -w -buildid="
		trimpath bool   = true
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"packages", &packages,
		"cwd?", &cwd,
		"ldflags?", &ldflags,
		"trimpath?", &trimpath,
	); err != nil {
		return nil, err
	}
	names, err := boot.StringList("packages", packages)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	if len(names) == 0 {
		return starlark.None, nil
	}
	latest := newLatestCache(latestModules(names))
	for _, name := range names {
		addInstallAction(thread, m.rt, name, cwd, ldflags, trimpath, "", latest)
	}
	return starlark.None, nil
}

func (m *impl) installLocal(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		pkg            string
		cwd            string
		fallbackLatest bool   = true
		ldflags        string = "-s -w -buildid="
		trimpath       bool   = true
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"package", &pkg,
		"cwd", &cwd,
		"fallback_latest?", &fallbackLatest,
		"ldflags?", &ldflags,
		"trimpath?", &trimpath,
	); err != nil {
		return nil, err
	}
	addInstallAction(thread, m.rt, pkg, cwd, ldflags, trimpath, cwd, nil)
	if fallbackLatest {
		addInstallAction(thread, m.rt, pkg+"@latest", "", ldflags, trimpath, "!"+cwd, newLatestCache([]string{packagePath(pkg)}))
	}
	return starlark.None, nil
}

func addInstallAction(thread *starlark.Thread, rt *boot.Runtime, pkg, cwd, ldflags string, trimpath bool, condition string, latest *latestCache) {
	args := []string{"install"}
	if ldflags != "" {
		args = append(args, "-ldflags="+ldflags)
	}
	if trimpath {
		args = append(args, "-trimpath")
	}
	args = append(args, pkg)

	summary := "go " + strings.Join(args, " ")
	if cwd != "" {
		summary += " (cwd " + rt.ResolveTarget(cwd) + ")"
	}

	boot.AddAction(thread, boot.Action{
		Summary: summary,
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			if condition != "" {
				negated := strings.HasPrefix(condition, "!")
				path := condition
				if negated {
					path = strings.TrimPrefix(path, "!")
				}
				_, err := os.Stat(rt.ResolveTarget(path))
				exists := err == nil
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					return "", err
				}
				if negated == exists {
					return boot.ResultSkip, nil
				}
			}
			upToDate, err := installUpToDate(ctx, rt, pkg, cwd, latest)
			if err != nil {
				return "", err
			}
			if upToDate {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			cmd := exec.CommandContext(ctx, "go", args...)
			if cwd != "" {
				cmd.Dir = rt.ResolveTarget(cwd)
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				msg := strings.TrimSpace(string(out))
				if msg == "" {
					return "", err
				}
				return "", fmt.Errorf("%w:\n%s", err, msg)
			}
			return boot.ResultChange, nil
		},
	})
}

func installUpToDate(ctx context.Context, rt *boot.Runtime, pkg, cwd string, latest *latestCache) (bool, error) {
	bin, err := binaryPath(ctx, pkg)
	if err != nil {
		return false, err
	}
	info, err := buildInfo(ctx, bin)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Path != packagePath(pkg) {
		return false, nil
	}
	if cwd != "" {
		return localUpToDate(ctx, rt.ResolveTarget(cwd), info)
	}
	return proxyUpToDate(ctx, pkg, info, latest)
}

func binaryPath(ctx context.Context, pkg string) (string, error) {
	name := pathBase(packagePath(pkg))
	out, err := exec.CommandContext(ctx, "go", "env", "GOBIN", "GOPATH", "GOEXE").Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for len(lines) < 3 {
		lines = append(lines, "")
	}
	if lines[0] != "" {
		return filepath.Join(lines[0], name+lines[2]), nil
	}
	gopath := strings.Split(lines[1], string(os.PathListSeparator))[0]
	if gopath == "" {
		return "", fmt.Errorf("go env GOPATH is empty")
	}
	return filepath.Join(gopath, "bin", name+lines[2]), nil
}

func packagePath(pkg string) string {
	path, _, _ := strings.Cut(pkg, "@")
	return path
}

func pathBase(path string) string {
	path = strings.TrimRight(path, "/")
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return path
	}
	return path[i+1:]
}

type goBuildInfo struct {
	Path     string
	Module   string
	Version  string
	Settings map[string]string
}

func buildInfo(_ context.Context, bin string) (goBuildInfo, error) {
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		if _, statErr := os.Stat(bin); os.IsNotExist(statErr) {
			return goBuildInfo{}, statErr
		}
		return goBuildInfo{}, nil
	}
	got := goBuildInfo{
		Path:     info.Path,
		Module:   info.Main.Path,
		Version:  info.Main.Version,
		Settings: make(map[string]string),
	}
	for _, setting := range info.Settings {
		got.Settings[setting.Key] = setting.Value
	}
	return got, nil
}

func localUpToDate(ctx context.Context, cwd string, info goBuildInfo) (bool, error) {
	head, err := output(ctx, cwd, "git", "rev-parse", "HEAD")
	if err != nil {
		return false, nil
	}
	dirty, err := output(ctx, cwd, "git", "status", "--porcelain")
	if err != nil {
		return false, nil
	}
	return info.Settings["vcs.revision"] == head && info.Settings["vcs.modified"] == "false" && dirty == "", nil
}

func proxyUpToDate(ctx context.Context, pkg string, info goBuildInfo, latest *latestCache) (bool, error) {
	if info.Module == "" || info.Version == "" {
		return false, nil
	}
	_, version, hasVersion := strings.Cut(pkg, "@")
	if hasVersion && version != "latest" {
		return info.Version == version, nil
	}
	var (
		latestInfo moduleInfo
		err        error
	)
	if latest != nil {
		latestInfo, err = latest.Get(ctx, info.Module)
	} else {
		latestInfo, err = latestModule(ctx, info.Module)
	}
	if err != nil {
		return false, err
	}
	return info.Version == latestInfo.Version, nil
}

type moduleInfo struct {
	Version string
}

func latestModule(ctx context.Context, module string) (moduleInfo, error) {
	escaped, err := modulepkg.EscapePath(module)
	if err != nil {
		return moduleInfo{}, err
	}
	info, err := request.Make[moduleInfo](ctx, request.Params{
		Method: http.MethodGet,
		URL:    strings.TrimRight(goProxyURL, "/") + "/" + escaped + "/@latest",
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
	})
	if err != nil {
		return moduleInfo{}, err
	}
	if info.Version == "" {
		return moduleInfo{}, fmt.Errorf("go list returned no latest version for %s", module)
	}
	return info, nil
}

type latestCache struct {
	modules []string
	once    sync.Once
	mu      sync.Mutex
	infos   map[string]moduleInfo
	errs    map[string]error
}

func newLatestCache(modules []string) *latestCache {
	return &latestCache{modules: modules}
}

// Get returns the latest module info for the given module, using cached values if available.
func (c *latestCache) Get(ctx context.Context, module string) (moduleInfo, error) {
	c.once.Do(func() {
		c.fetch(ctx)
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.errs[module]; err != nil {
		return moduleInfo{}, err
	}
	info, ok := c.infos[module]
	if !ok {
		var err error
		info, err = latestModule(ctx, module)
		if err != nil {
			return moduleInfo{}, err
		}
		c.infos[module] = info
	}
	return info, nil
}

func (c *latestCache) fetch(ctx context.Context) {
	c.mu.Lock()
	c.infos = make(map[string]moduleInfo)
	c.errs = make(map[string]error)
	c.mu.Unlock()

	g, ctx := errgroup.WithContext(ctx)
	for _, module := range c.modules {
		g.Go(func() error {
			info, err := latestModule(ctx, module)
			c.mu.Lock()
			defer c.mu.Unlock()
			if err != nil {
				c.errs[module] = err
				return nil
			}
			c.infos[module] = info
			return nil
		})
	}

	g.Wait()
}

func latestModules(packages []string) []string {
	seen := make(map[string]bool)
	var modules []string
	for _, pkg := range packages {
		_, version, ok := strings.Cut(pkg, "@")
		if !ok || version != "latest" {
			continue
		}
		module := packagePath(pkg)
		if !seen[module] {
			seen[module] = true
			modules = append(modules, module)
		}
	}
	return modules
}

func output(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
