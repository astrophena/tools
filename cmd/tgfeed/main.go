// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bufio"
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/tools/cmd/tgfeed/internal/diff"

	"github.com/mmcdole/gofeed"
)

const tgAPI = "https://api.telegram.org"

//go:embed error.tmpl
var defaultErrorTemplate string

// Some types of errors that can happen during tgfeed execution.
var (
	errAlreadyRunning = errors.New("already running")
	errNoFeed         = errors.New("no such feed")
	errNoEditor       = errors.New("environment variable EDITOR is not defined")
)

func main() { cli.Main(new(fetcher)) }

func (f *fetcher) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&f.dry, "dry", false, "Enable dry-run mode: log actions, but don't send updates or save state.")
	fs.BoolVar(&f.json, "json", false, "Output in JSON format (honored in supported commands).")
	fs.StringVar(&f.remoteURL, "remote", "", "Remote admin API URL (e.g., 'http://localhost:8080' or '/run/tgfeed/admin-socket').")
}

func (f *fetcher) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	// Load configuration from environment variables.
	f.adminAddr = cmp.Or(f.adminAddr, env.Getenv("ADMIN_ADDR"), "localhost:3000")
	f.chatID = cmp.Or(f.chatID, env.Getenv("CHAT_ID"))
	f.errorThreadID = cmp.Or(f.errorThreadID, parseInt(env.Getenv("ERROR_THREAD_ID")))
	f.ghToken = cmp.Or(f.ghToken, env.Getenv("GITHUB_TOKEN"))
	f.stateDir = cmp.Or(f.stateDir, env.Getenv("STATE_DIRECTORY"))
	if f.stateDir == "" {
		xdgStateHome := env.Getenv("XDG_STATE_HOME")
		if xdgStateHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			xdgStateHome = filepath.Join(home, ".local", "state")
		}
		f.stateDir = filepath.Join(xdgStateHome, "tgfeed")
		if err := os.MkdirAll(f.stateDir, 0o700); err != nil {
			return err
		}
	}
	f.tgToken = cmp.Or(f.tgToken, env.Getenv("TELEGRAM_TOKEN"))

	// Initialize internal state.
	f.init.Do(func() {
		f.doInit(ctx)
	})

	// Enable debug logging in dry-run mode.
	if f.dry {
		f.slogLevel.Set(slog.LevelDebug)
	}

	if len(env.Args) == 0 {
		return fmt.Errorf("%w: command is required, see -help for usage", cli.ErrInvalidArgs)
	}
	command := env.Args[0]

	switch command {
	case "admin":
		return f.admin(ctx)
	case "feeds":
		return f.listFeeds(ctx, env.Stdout)
	case "edit":
		return f.edit(ctx)
	case "run":
		if err := f.run(ctx); err != nil {
			return f.errNotify(ctx, err)
		}
		return nil
	case "reenable":
		if len(env.Args) != 2 {
			return fmt.Errorf("%w: reenable command expects a feed URL", cli.ErrInvalidArgs)
		}
		return f.reenable(ctx, env.Args[1])
	default:
		return fmt.Errorf("%w: no such command %q", cli.ErrInvalidArgs, command)
	}
}

func parseInt(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return i
	}
	return 0
}

type fetcher struct {
	running atomic.Bool
	init    sync.Once

	// configuration
	adminAddr     string
	chatID        string
	dry           bool
	json          bool
	errorThreadID int64
	// now acts as time.Now, but can be mocked for testing.
	now       func() time.Time
	ghToken   string
	remoteURL string
	stateDir  string
	tgToken   string

	// initialized by doInit
	fp        *gofeed.Parser
	httpc     *http.Client
	logf      func(string, ...any)
	scrubber  *strings.Replacer
	slog      *slog.Logger
	slogLevel *slog.LevelVar

	// loaded from state
	config        string
	feeds         []*feed
	errorTemplate string
	state         syncx.Protected[map[string]*feedState]

	stats syncx.Protected[*stats]
}

func (f *fetcher) doInit(ctx context.Context) {
	env := cli.GetEnv(ctx)
	f.logf = log.New(env.Stderr, "", 0).Printf
	if f.now == nil {
		f.now = time.Now
	}

	if f.httpc == nil {
		f.httpc = request.DefaultClient
	}

	f.fp = gofeed.NewParser()

	if f.tgToken != "" {
		f.scrubber = strings.NewReplacer(
			f.tgToken, "[EXPUNGED]",
		)
	}

	l := logger.Get(ctx)
	f.slogLevel = l.Level
	f.slog = l.Logger
}

func (f *fetcher) listFeeds(ctx context.Context, w io.Writer) error {
	if err := f.loadState(ctx); err != nil {
		return err
	}

	if f.json {
		type feedJSON struct {
			URL     string     `json:"url"`
			Title   string     `json:"title,omitempty"`
			State   *feedState `json:"state,omitempty"`
			NoState bool       `json:"no_state,omitempty"`
		}

		var feeds []feedJSON
		for _, feed := range f.feeds {
			state, hasState := f.getState(feed.url)
			fj := feedJSON{
				URL:   feed.url,
				Title: feed.title,
				State: state,
			}
			if !hasState {
				fj.NoState = true
			}
			feeds = append(feeds, fj)
		}

		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(feeds)
	}

	const (
		colorReset  = "\033[0m"
		colorRed    = "\033[31m"
		colorGreen  = "\033[32m"
		colorYellow = "\033[33m"
		colorBlue   = "\033[34m"
		colorGray   = "\033[90m"
	)

	fmt.Fprintln(w, "STATE  FEED                                      UPDATED   FAIL%")

	var (
		total    int
		healthy  int
		failing  int
		disabled int
	)

	for _, feed := range f.feeds {
		total++
		state, hasState := f.getState(feed.url)

		var (
			stateStr   string
			feedStr    string
			updatedStr string
			failRate   string
		)

		if feed.title != "" {
			feedStr = feed.title
		} else {
			feedStr = feed.url
		}
		// Truncate feed name/URL to ~40 chars to keep it compact.
		if utf8.RuneCountInString(feedStr) > 40 {
			feedStr = string([]rune(feedStr)[:37]) + "..."
		}

		if !hasState {
			updatedStr = "-"
			stateStr = colorGray + "NO STATE" + colorReset
		} else {
			updatedStr = relativeTime(state.LastUpdated, f.now())

			if state.Disabled {
				disabled++
				stateStr = colorGray + "OFF" + colorReset
				feedStr = colorGray + feedStr + colorReset
			} else if state.FetchFailCount > 0 || state.ErrorCount > 0 {
				failing++
				stateStr = colorRed + "ERR" + colorReset
				if state.FetchFailCount > 0 && state.FetchCount > 0 {
					rate := (float32(state.FetchFailCount) / float32(state.FetchCount)) * 100
					if rate > 0 {
						failRate = fmt.Sprintf("%.0f%%", rate)
					}
				}
			} else {
				healthy++
				stateStr = colorGreen + "OK" + colorReset
			}
		}

		// STATE (6) | FEED (42) | UPDATED (10) | FAIL%
		fmt.Fprintf(w, "%s%s%s%s\n",
			pad(stateStr, 7),
			pad(feedStr, 42),
			pad(updatedStr, 10),
			failRate,
		)
	}

	fmt.Fprintf(w, "\nSummary: %d total, %s%d healthy%s, %s%d failing%s, %s%d disabled%s\n",
		total,
		colorGreen, healthy, colorReset,
		colorRed, failing, colorReset,
		colorGray, disabled, colorReset,
	)

	return nil
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func pad(s string, width int) string {
	l := utf8.RuneCountInString(stripANSI(s))
	if l >= width {
		return s
	}
	return s + strings.Repeat(" ", width-l)
}

func relativeTime(t, now time.Time) string {
	d := now.Sub(t)
	if d < time.Second {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d < 48*time.Hour {
		return "yesterday"
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (f *fetcher) edit(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	editor := env.Getenv("EDITOR")
	if editor == "" {
		return errNoEditor
	}

	if err := f.loadState(ctx); err != nil {
		return err
	}

	tmpfile, err := os.CreateTemp("", "tgfeed-config*.star")
	if err != nil {
		return err
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(f.config); err != nil {
		return err
	}
	if err := tmpfile.Close(); err != nil {
		return err
	}

	for {
		cmd := exec.Command(editor, tmpfile.Name())
		cmd.Stdin = env.Stdin
		cmd.Stdout = env.Stdout
		cmd.Stderr = env.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}

		edited, err := os.ReadFile(tmpfile.Name())
		if err != nil {
			return err
		}
		if string(edited) == f.config {
			f.logf("No changes made to config.star, exiting.")
			return nil
		}

		f.logf("You've made these changes:")
		f.logf("%s", diff.Diff("old", []byte(f.config), "new", edited))
		if !f.ask("Do you want to save?", env.Stdin) {
			return nil
		}

		_, err = f.parseConfig(ctx, string(edited))
		if err != nil {
			f.logf("Invalid config.star: %v", err)
			if f.ask("Do you want to try editing again?", env.Stdin) {
				continue
			}
			return err
		}

		f.config = string(edited)
		break
	}

	return f.saveConfig(ctx)
}

// ask prompts the user for a yes or no answer.
func (f *fetcher) ask(prompt string, stdin io.Reader) bool {
	r := bufio.NewReader(stdin)
	for {
		fmt.Printf("%s (y/n): ", prompt)
		input, err := r.ReadString('\n')
		if err != nil {
			f.logf("Error reading input.")
			continue
		}

		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
		f.logf("Invalid input (y/n).")
	}
}

func (f *fetcher) run(ctx context.Context) error {
	// Check if this fetcher is already running.
	if f.running.Load() {
		return errAlreadyRunning
	}
	f.running.Store(true)
	defer f.running.Store(false)

	// Acquire run lock to prevent concurrent state modifications.
	if err := f.acquireRunLock(); err != nil {
		return err
	}
	defer f.releaseRunLock()

	// Start with empty stats for every run.
	f.stats = syncx.Protect(&stats{
		StartTime: time.Now(),
	})

	if err := f.loadState(ctx); err != nil {
		return fmt.Errorf("loading state failed: %w", err)
	}

	// Recreate updates channel on each fetch.
	updates := make(chan *update)

	var baseWg sync.WaitGroup

	// Start sending goroutine.
	baseWg.Go(func() {
		sendWg := syncx.NewLimitedWaitGroup(sendConcurrencyLimit)

	loop:
		for {
			select {
			case feedItem, valid := <-updates:
				if !valid {
					break loop
				}
				sendWg.Go(func() { f.sendUpdate(ctx, feedItem) })
			case <-ctx.Done():
				break loop
			}
		}

		sendWg.Wait()
	})

	var fetchedFeeds atomic.Int64

	// Enqueue fetches.
	fetchWg := syncx.NewLimitedWaitGroup(fetchConcurrencyLimit)
	for _, feed := range shuffle(f.feeds) {
		fetchWg.Go(func() {
			defer fetchedFeeds.Add(1)

			var retries int

			for {
				if retry, retryIn := f.fetch(ctx, feed, updates); retry && retries < retryLimit {
					f.slog.Warn("retrying feed",
						"feed", feed.url,
						"retry_in", retryIn,
						"retries", retries+1,
						"retry_limit", retryLimit,
					)
					time.Sleep(retryIn)
					retries += 1
					continue
				}
				break
			}
		})
	}

	// Wait for all fetches to complete.
	fetchWg.Wait()
	// Stop sending goroutine.
	close(updates)
	// Wait for sending goroutine to finish.
	baseWg.Wait()

	// Prepare and report stats.

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	f.stats.WriteAccess(func(s *stats) {
		s.Duration = time.Since(s.StartTime)
		s.TotalFeeds = len(f.feeds)
		if s.SuccessFeeds > 0 {
			s.AvgFetchTime = s.TotalFetchTime / time.Duration(s.SuccessFeeds)
		}
		s.MemoryUsage = m.Alloc
	})

	f.state.WriteAccess(f.cleanState)

	f.slog.Debug("fetch finished", "fetched_count", fetchedFeeds.Load(), "all_count", len(f.feeds))

	if f.dry {
		return nil
	}

	f.stats.ReadAccess(func(s *stats) {
		if err := f.putStats(ctx, s); err != nil {
			f.slog.Warn("failed to upload stats", "error", err)
		}
	})

	return f.saveState(ctx)
}

func (f *fetcher) cleanState(s map[string]*feedState) {
	for url := range s {
		var found bool
		for _, existing := range f.feeds {
			if url == existing.url {
				found = true
				break
			}
		}
		if !found {
			f.slog.Debug("removing state, feed no longer exists", "feed", url)
			delete(s, url)
		}
	}
}

func shuffle[S any](s []S) []S {
	s2 := slices.Clone(s)
	rand.Shuffle(len(s2), func(i, j int) {
		s2[i], s2[j] = s2[j], s2[i]
	})
	return s2
}

func (f *fetcher) reenable(ctx context.Context, url string) error {
	if err := f.loadState(ctx); err != nil {
		return err
	}

	state, ok := f.getState(url)
	if !ok {
		return fmt.Errorf("%q: %w", url, errNoFeed)
	}

	state.Disabled = false
	state.ErrorCount = 0
	state.LastError = ""

	return f.saveState(ctx)
}
