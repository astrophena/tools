// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bufio"
	"cmp"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/tools/cmd/tgfeed/internal/diff"
	"go.astrophena.name/tools/internal/api/github/gist"
	"go.astrophena.name/tools/internal/api/google/serviceaccount"

	"github.com/lmittmann/tint"
	"github.com/mmcdole/gofeed"
)

const (
	ghAPI = "https://api.github.com"
	tgAPI = "https://api.telegram.org"
)

//go:embed error.tmpl
var defaultErrorTemplate string

// Some types of errors that can happen during tgfeed execution.
var (
	errAlreadyRunning      = errors.New("already running")
	errNoFeed              = errors.New("no such feed")
	errNoEditor            = errors.New("environment variable EDITOR is not defined")
	errNoServiceAccountKey = errors.New("no service account key")
)

func main() { cli.Main(new(fetcher)) }

func (f *fetcher) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&f.dry, "dry", false, "Enable dry-run mode: log actions, but don't send updates or save state.")
	fs.BoolVar(&f.jsonLog, "json-log", false, "Emit logs in JSON format.")
}

func (f *fetcher) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)
	f.logf = env.Logf

	// Load configuration from environment variables.
	f.chatID = cmp.Or(f.chatID, env.Getenv("CHAT_ID"))
	f.errorThreadID = cmp.Or(f.errorThreadID, parseInt(env.Getenv("ERROR_THREAD_ID")))
	f.ghToken = cmp.Or(f.ghToken, env.Getenv("GITHUB_TOKEN"))
	f.gistID = cmp.Or(f.gistID, env.Getenv("GIST_ID"))
	f.statsSpreadsheetID = cmp.Or(f.statsSpreadsheetID, env.Getenv("STATS_SPREADSHEET_ID"))
	f.statsSpreadsheetSheet = cmp.Or(f.statsSpreadsheetSheet, env.Getenv("STATS_SPREADSHEET_SHEET"), "Stats")
	f.tgToken = cmp.Or(f.tgToken, env.Getenv("TELEGRAM_TOKEN"))

	// Load Google service account key from SERVICE_ACCOUNT_KEY environment
	// variable. If it's not defined, stats won't be uploaded to a Google
	// spreadsheet.
	if key := env.Getenv("SERVICE_ACCOUNT_KEY"); key != "" {
		var err error
		f.serviceAccountKey, err = serviceaccount.LoadKey([]byte(key))
		if err != nil {
			return err
		}
	}

	// Initialize internal state.
	f.init.Do(f.doInit)

	// Enable debug logging in dry-run mode.
	if f.dry {
		f.slogLevel.Set(slog.LevelDebug)
	}

	if len(env.Args) == 0 {
		return fmt.Errorf("%w: command is required, see -help for usage", cli.ErrInvalidArgs)
	}
	command := env.Args[0]

	switch command {
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
	// Internal command, used in jobs/tgfeed/pprof-upload.bash.
	case "google-token":
		if len(env.Args) != 2 {
			return fmt.Errorf("%w: google-token command expects a scope", cli.ErrInvalidArgs)
		}
		if f.serviceAccountKey == nil {
			return errNoServiceAccountKey
		}
		tok, err := f.serviceAccountKey.AccessToken(ctx, f.httpc, env.Args[1])
		if err != nil {
			return err
		}
		fmt.Println(tok)
		return nil
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
	chatID                string
	dry                   bool
	errorThreadID         int64
	ghToken               string
	gistID                string
	jsonLog               bool
	logf                  logger.Logf
	serviceAccountKey     *serviceaccount.Key
	statsSpreadsheetID    string
	statsSpreadsheetSheet string
	tgToken               string

	// initialized by doInit
	fp        *gofeed.Parser
	httpc     *http.Client
	scrubber  *strings.Replacer
	gistc     *gist.Client
	slog      *slog.Logger
	slogLevel *slog.LevelVar

	// loaded from Gist
	config        string
	feeds         []*feed
	errorTemplate string
	state         syncx.Protected[map[string]*feedState]

	stats syncx.Protected[*stats]
}

func (f *fetcher) doInit() {
	if f.logf == nil {
		panic("f.logf is nil")
	}

	if f.httpc == nil {
		f.httpc = request.DefaultClient
	}

	f.fp = gofeed.NewParser()

	if f.ghToken != "" && f.tgToken != "" {
		f.scrubber = strings.NewReplacer(
			f.ghToken, "[EXPUNGED]",
			f.tgToken, "[EXPUNGED]",
		)
	}

	f.gistc = &gist.Client{
		Token:      f.ghToken,
		HTTPClient: f.httpc,
		Scrubber:   f.scrubber,
	}

	f.slogLevel = new(slog.LevelVar)
	if f.jsonLog {
		f.slog = slog.New(slog.NewJSONHandler(f.logf, &slog.HandlerOptions{
			Level: f.slogLevel,
		}))
	} else {
		f.slog = slog.New(tint.NewHandler(f.logf, &tint.Options{
			Level: f.slogLevel,
		}))
	}
}

func (f *fetcher) listFeeds(ctx context.Context, w io.Writer) error {
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}

	var sb strings.Builder

	for _, feed := range f.feeds {
		state, hasState := f.getState(feed.URL)
		fmt.Fprintf(&sb, "%s", feed.URL)
		if !hasState {
			fmt.Fprintf(&sb, " \n")
			continue
		}
		fmt.Fprintf(&sb, " (")
		if feed.Title != "" {
			fmt.Fprintf(&sb, "%q, ", feed.Title)
		}
		fmt.Fprintf(&sb, "last updated %s", state.LastUpdated.Format(time.DateTime))
		if state.ErrorCount > 0 {
			fmt.Fprintf(&sb, ", failed %s, last error was %q", pluralize(int64(state.ErrorCount)), state.LastError)
		}
		if state.FetchCount > 0 {
			fmt.Fprintf(&sb, ", fetched %s", pluralize(state.FetchCount))
			if state.FetchFailCount > 0 {
				failRate := (float32(state.FetchFailCount) / float32(state.FetchCount)) * 100
				fmt.Fprintf(&sb, ", failure rate %.2f%%", failRate)
			}
		}
		if state.Disabled {
			fmt.Fprintf(&sb, ", disabled")
		}
		fmt.Fprintf(&sb, ")\n")
	}

	io.WriteString(w, sb.String())
	return nil
}

func pluralize(n int64) string {
	if n > 1 {
		return fmt.Sprintf("%d times", n)
	}
	return "once"
}

func (f *fetcher) edit(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	editor := env.Getenv("EDITOR")
	if editor == "" {
		return errNoEditor
	}

	if err := f.loadFromGist(ctx); err != nil {
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
		f.logf(string(diff.Diff("old", []byte(f.config), "new", edited)))
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

	return f.saveToGist(ctx)
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

		if input == "y" || input == "yes" {
			return true
		} else if input == "n" || input == "no" {
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

	// Start with empty stats for every run.
	f.stats = syncx.Protect(&stats{
		StartTime: time.Now(),
	})

	if err := f.loadFromGist(ctx); err != nil {
		return fmt.Errorf("fetching gist failed: %w", err)
	}

	// Recreate updates channel on each fetch.
	updates := make(chan *item)

	var baseWg sync.WaitGroup

	// Start sending goroutine.
	baseWg.Add(1)
	go func() {
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
		baseWg.Done()
	}()

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
						"feed", feed.URL,
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

	if f.serviceAccountKey != nil && f.statsSpreadsheetID != "" {
		token, err := f.serviceAccountKey.AccessToken(ctx, f.httpc, spreadsheetsScope)
		if err != nil {
			return err
		}
		f.stats.ReadAccess(func(s *stats) {
			if err := f.uploadStatsToSheets(ctx, token, s); err != nil {
				f.slog.Warn("failed to upload stats", "error", err)
			}
		})
	}

	return f.saveToGist(ctx)
}

func (f *fetcher) cleanState(s map[string]*feedState) {
	for url := range s {
		var found bool
		for _, existing := range f.feeds {
			if url == existing.URL {
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
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}

	state, ok := f.getState(url)
	if !ok {
		return fmt.Errorf("%q: %w", url, errNoFeed)
	}

	state.Disabled = false
	state.ErrorCount = 0
	state.LastError = ""

	return f.saveToGist(ctx)
}
