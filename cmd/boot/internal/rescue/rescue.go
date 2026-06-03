// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package rescue

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark rescue module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "rescue" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"update": starlark.NewBuiltin("rescue.update", m.update),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) update(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := boot.RequireTask(thread, b); err != nil {
		return nil, err
	}
	var (
		source string
		espDir string = "/efi/EFI/Linux"
		keep   int    = 3
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"source", &source,
		"esp_dir?", &espDir,
		"keep?", &keep,
	); err != nil {
		return nil, err
	}
	src := m.rt.ResolveSource(source)
	esp := m.rt.ResolveTarget(espDir)

	boot.AddAction(thread, boot.Action{
		Summary: fmt.Sprintf("update rescue image from %s", src),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			if _, err := os.Stat(src); err != nil {
				return "", err
			}
			if _, err := os.Stat(esp); err != nil {
				return boot.ResultSkip, nil
			}
			images, err := rescueImages(esp)
			if err != nil {
				return "", err
			}
			if len(images) > 0 && sameMonth(images[0].modTime, time.Now()) {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			if err := run(ctx, false, src, "mkosi", "build"); err != nil {
				return "", err
			}
			built, err := latestBuiltImage(filepath.Join(src, "mkosi.output"))
			if err != nil {
				return "", err
			}
			target := filepath.Join(esp, filepath.Base(built))
			if err := run(ctx, m.rt.NeedsSudo(), "", "cp", built, target); err != nil {
				return "", err
			}
			if err := run(ctx, m.rt.NeedsSudo(), "", "sbctl", "sign", "-s", target); err != nil {
				run(ctx, m.rt.NeedsSudo(), "", "rm", "-f", target)
				return "", err
			}
			if err := prune(ctx, m.rt, esp, keep); err != nil {
				return "", err
			}
			if err := pruneOutput(ctx, m.rt, filepath.Join(src, "mkosi.output"), keep); err != nil {
				return "", err
			}
			return boot.ResultChange, nil
		},
	})
	return starlark.None, nil
}

type image struct {
	path    string
	modTime time.Time
}

func rescueImages(dir string) ([]image, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var images []image
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "arch-linux-rescue_") || !strings.HasSuffix(entry.Name(), ".efi") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		images = append(images, image{path: filepath.Join(dir, entry.Name()), modTime: info.ModTime()})
	}
	slices.SortFunc(images, func(a, b image) int { return b.modTime.Compare(a.modTime) })
	return images, nil
}

func latestBuiltImage(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "arch-linux-rescue_*.efi"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no built rescue image found in %s", dir)
	}
	slices.SortFunc(matches, func(a, b string) int {
		ai, aerr := os.Stat(a)
		bi, berr := os.Stat(b)
		if aerr != nil || berr != nil {
			return strings.Compare(b, a)
		}
		return bi.ModTime().Compare(ai.ModTime())
	})
	return matches[0], nil
}

func sameMonth(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month()
}

func prune(ctx context.Context, rt *boot.Runtime, esp string, keep int) error {
	if keep <= 0 {
		return nil
	}
	images, err := rescueImages(esp)
	if err != nil {
		return err
	}
	for _, image := range images[keep:] {
		if err := run(ctx, rt.NeedsSudo(), "", "rm", "-f", image.path); err != nil {
			return err
		}
		run(ctx, rt.NeedsSudo(), "", "sbctl", "remove-file", image.path)
	}
	return nil
}

var rescueOutputPattern = regexp.MustCompile(`arch-linux-rescue_(\d{12})`)

type outputBuild struct {
	stamp string
	paths []string
}

func rescueOutputBuilds(dir string) ([]outputBuild, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	builds := make(map[string][]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := rescueOutputPattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		stamp := match[1]
		builds[stamp] = append(builds[stamp], filepath.Join(dir, entry.Name()))
	}
	out := make([]outputBuild, 0, len(builds))
	for stamp, paths := range builds {
		slices.Sort(paths)
		out = append(out, outputBuild{stamp: stamp, paths: paths})
	}
	slices.SortFunc(out, func(a, b outputBuild) int { return strings.Compare(b.stamp, a.stamp) })
	return out, nil
}

func pruneOutput(ctx context.Context, rt *boot.Runtime, dir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	builds, err := rescueOutputBuilds(dir)
	if err != nil {
		return err
	}
	for _, build := range builds[keep:] {
		for _, path := range build.paths {
			if err := run(ctx, rt.NeedsSudo(), "", "rm", "-f", path); err != nil {
				return err
			}
		}
	}
	return nil
}

func run(ctx context.Context, sudo bool, dir, name string, args ...string) error {
	argv := append([]string{name}, args...)
	if sudo {
		argv = append([]string{"sudo"}, argv...)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	return boot.RunCmd(cmd)
}
