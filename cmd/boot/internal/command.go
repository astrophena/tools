// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// RunCommand runs argv and includes combined output in returned errors.
func RunCommand(ctx context.Context, dir string, argv ...string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return CommandError(argv, buf.Bytes(), err)
	}
	return nil
}

// CommandError formats a command failure with trimmed command output.
func CommandError(argv []string, out []byte, err error) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return fmt.Errorf("%s failed: %w", strings.Join(argv, " "), err)
	}
	return fmt.Errorf("%s failed: %w:\n%s", strings.Join(argv, " "), err, msg)
}
