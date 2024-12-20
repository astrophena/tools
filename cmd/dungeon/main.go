// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"go.astrophena.name/tools/internal/cli"

	"github.com/landlock-lsm/go-landlock/landlock"
)

func main() { cli.Main(new(app)) }

type app struct {
	configPath string
}

type config struct {
	FS struct {
		// Directories that sandboxed program are allowed to only read.
		RODirs []string `json:"ro_dirs"`
		// Directories that sandboxed program are allowed to read and write.
		RWDirs []string `json:"rw_dirs"`
		// Files that sandboxed program are allowed to only read.
		ROFiles []string `json:"ro_files"`
		// Files that sandboxed program are allowed to read and write.
		RWFiles []string `json:"rw_files"`
	} `json:"fs"`
	Network struct {
		// TCP ports that sandboxed program are allowed to connect.
		AllowedPorts []uint16 `json:"allowed_ports"`
		// TCP ports that sandboxed program are allowed to bind.
		AllowedBindings []uint16 `json:"allowed_bindings"`
	} `json:"network"`
}

func (a *app) Flags(fs *flag.FlagSet) {
	fs.StringVar(&a.configPath, "config", "", "Path to config file.")
}

func (a *app) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	if len(env.Args) == 0 {
		return fmt.Errorf("%w: at least program name is required", cli.ErrInvalidArgs)
	}

	if a.configPath == "" {
		return fmt.Errorf("%w: set -config flag to config file path", cli.ErrInvalidArgs)
	}
	b, err := os.ReadFile(a.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	var cfg config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	var (
		rules       []landlock.Rule
		restrictNet bool
	)

	if len(cfg.FS.RODirs) > 0 {
		rules = append(rules, landlock.RODirs(cfg.FS.RODirs...))
	}
	if len(cfg.FS.RWDirs) > 0 {
		rules = append(rules, landlock.RWDirs(cfg.FS.RWDirs...))
	}
	if len(cfg.Network.AllowedBindings) > 0 {
		restrictNet = true
		for _, port := range cfg.Network.AllowedBindings {
			rules = append(rules, landlock.BindTCP(port))
		}
	}
	if len(cfg.Network.AllowedPorts) > 0 {
		restrictNet = true
		for _, port := range cfg.Network.AllowedPorts {
			rules = append(rules, landlock.ConnectTCP(port))
		}
	}

	restrict := landlock.V4.RestrictPaths
	if restrictNet {
		restrict = landlock.V4.Restrict
	}

	if err := restrict(rules...); err != nil {
		return fmt.Errorf("restricting ourselves with Landlock failed: %w", err)
	}

	cmd := exec.CommandContext(ctx, env.Args[0], env.Args[1:]...)
	cmd.Stdout = env.Stdout
	cmd.Stderr = env.Stderr
	cmd.Stdin = env.Stdin

	return cmd.Run()
}
