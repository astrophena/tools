// cmdtop displays the top of most used commands in bash history.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.astrophena.name/tools/internal/cli"
)

func main() {
	cli.SetDescription("cmdtop displays the top of most used commands in bash history.")
	cli.SetArgsUsage("[num] [flags]")
	cli.HandleStartup()

	num := int64(10)
	args := flag.Args()
	if len(args) > 0 {
		var err error
		num, err = strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			log.Fatalf("Invalid number of commands: %v", err)
		}
	}

	histfile, ok := os.LookupEnv("HISTFILE")
	if !ok {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		histfile = filepath.Join(home, ".bash_history")
	}

	f, err := os.Open(histfile)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	top, err := count(f, num)
	if err != nil {
		log.Fatal(err)
	}
	io.WriteString(os.Stdout, top)
}

func count(r io.Reader, num int64) (top string, err error) {
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
		return "", err
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

	var sb strings.Builder
	for i, kv := range ss {
		if int64(i) == num {
			break
		}
		fmt.Fprintf(&sb, "%d. %s (%d)\n", i+1, kv.key, kv.value)
	}

	return sb.String(), nil
}
