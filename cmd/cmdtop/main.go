// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Cmdtop displays the top of most used commands in bash history.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.astrophena.name/tools/internal/cli"
)

func main() { cli.Run(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr)) }

var errInvalidNum = errors.New("invalid number of commands")

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	a := &cli.App{
		Name:        "cmdtop",
		Description: helpDoc,
		ArgsUsage:   "[flags...] [num]",
	}
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	num := int64(10)
	if len(a.Flags.Args()) > 0 {
		var err error
		num, err = strconv.ParseInt(a.Flags.Args()[0], 10, 64)
		if err != nil {
			return fmt.Errorf("%w: %v", errInvalidNum, err)
		}
	}

	var histfile string
	if histfile = getenv("HISTFILE"); histfile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to lookup home directory: %w", err)
		}
		histfile = filepath.Join(home, ".bash_history")
	}

	f, err := os.Open(histfile)
	if err != nil {
		return err
	}
	defer f.Close()

	top, err := count(f, num)
	if err != nil {
		return err
	}
	_, err = stdout.Write(top)
	return err
}

func count(r io.Reader, num int64) (top []byte, err error) {
	scanner := bufio.NewScanner(r)

	m := make(map[string]int)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "#") {
			continue
		}
		cmd := strings.Fields(scanner.Text())
		if len(cmd) > 0 && cmd[0] != "" {
			m[cmd[0]]++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	type kv struct {
		key   string
		value int
	}
	var ss []kv
	for k, v := range m {
		ss = append(ss, kv{k, v})
	}
	sort.Slice(ss, func(i, j int) bool {
		if ss[i].value != ss[j].value {
			return ss[i].value > ss[j].value
		}
		return ss[i].key > ss[j].key
	})

	var b bytes.Buffer
	for i, kv := range ss {
		if int64(i) == num {
			break
		}
		fmt.Fprintf(&b, "%d. %s (%d)\n", i+1, kv.key, kv.value)
	}

	return b.Bytes(), nil
}
