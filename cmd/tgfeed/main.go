// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/tools/cmd/tgfeed/internal/diff"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/api/google/serviceaccount"

	"github.com/mmcdole/gofeed"
)

const (
	defaultErrorTemplate = `❌ Something went wrong:
<pre><code>%v</code></pre>`
	ghAPI = "https://api.github.com"
	tgAPI = "https://api.telegram.org"
)

// Some types of errors that can happen during tgfeed execution.
var (
	errAlreadyRunning = errors.New("already running")
	errNoFeed         = errors.New("no such feed")
	errNoEditor       = errors.New("environment variable EDITOR is not defined")
)

func main() { cli.Main(new(fetcher)) }

func (f *fetcher) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&f.dry, "dry", false, "Don't send updates and update state when running, log everything instead.")
	fs.BoolVar(&f.mode.feeds, "feeds", false, "List available feeds.")
	fs.BoolVar(&f.mode.edit, "edit", false, "Edit config.star file in your EDITOR.")
	fs.StringVar(&f.mode.reenable, "reenable", "", "Reenable disabled `feed`.")
	fs.BoolVar(&f.mode.run, "run", false, "Fetch feeds and send updates.")
}

func (f *fetcher) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	// Initialize logger.
	f.logf = env.Logf

	// Load configuration from environment variables.
	f.chatID = cmp.Or(f.chatID, env.Getenv("CHAT_ID"))
	f.ghToken = cmp.Or(f.ghToken, env.Getenv("GITHUB_TOKEN"))
	f.gistID = cmp.Or(f.gistID, env.Getenv("GIST_ID"))
	f.statsSpreadsheetID = cmp.Or(f.statsSpreadsheetID, env.Getenv("STATS_SPREADSHEET_ID"))
	f.statsSpreadsheetRange = cmp.Or(f.statsSpreadsheetRange, env.Getenv("STATS_SPREADSHEET_RANGE"), "Stats")
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

	// Choose a mode based on passed flags and run it.
	switch {
	case f.mode.feeds:
		return f.listFeeds(ctx, env.Stdout)
	case f.mode.edit:
		return f.edit(ctx)
	case f.mode.run:
		if err := f.run(ctx); err != nil {
			return f.errNotify(ctx, err)
		}
		return nil
	case f.mode.reenable != "":
		return f.reenable(ctx, f.mode.reenable)
	default:
		return fmt.Errorf("%w: pick a mode", cli.ErrInvalidArgs)
	}
}

type fetcher struct {
	running atomic.Bool
	init    sync.Once

	// configuration
	mode struct {
		feeds    bool
		edit     bool
		reenable string
		run      bool
	}
	chatID                string
	dry                   bool
	ghToken               string
	gistID                string
	logf                  logger.Logf
	serviceAccountKey     *serviceaccount.Key
	statsSpreadsheetID    string
	statsSpreadsheetRange string
	tgToken               string

	// initialized by doInit
	fp       *gofeed.Parser
	httpc    *http.Client
	scrubber *strings.Replacer
	gistc    *gist.Client

	// loaded from Gist
	config        string
	feeds         []*feed
	errorTemplate string
	state         *syncx.Protected[map[string]*feedState]

	stats *syncx.Protected[*stats]
}

func (f *fetcher) dlogf(format string, args ...any) {
	if !f.dry {
		return
	}
	f.logf(format, args...)
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
			f.logf("No changes made to config.star, not doing anything.")
			return nil
		}

		f.logf("You've made these changes:")
		f.logf(string(diff.Diff("config.star", []byte(f.config), "config.star", edited)))
		if !f.ask("Do you want to save?", env.Stdin) {
			return nil
		}

		_, err = f.parseConfig(string(edited))
		if err != nil {
			f.logf("Edited file is invalid: %v.", err)
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
			f.logf("Error reading input, please try again.")
			continue
		}

		input = strings.TrimSpace(strings.ToLower(input))

		if input == "y" || input == "yes" {
			return true
		} else if input == "n" || input == "no" {
			return false
		}
		f.logf("Invalid input. Please enter 'y' or 'n'.")
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
	updates := make(chan *gofeed.Item)

	var baseWg sync.WaitGroup

	// Start sending goroutine.
	baseWg.Add(1)
	go func() {
		sendWg := syncx.NewLimitedWaitGroup(sendConcurrencyLimit)

	loop:
		for {
			select {
			case item, valid := <-updates:
				if !valid {
					break loop
				}

				sendWg.Add(1)
				go func() {
					defer sendWg.Done()
					f.sendUpdate(ctx, item)
				}()
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
		fetchWg.Add(1)
		go func() {
			defer fetchWg.Done()
			defer fetchedFeeds.Add(1)

			var retries int

			for {
				if retry, retryIn := f.fetch(ctx, feed, updates); retry && retries < retryLimit {
					f.logf("Retrying feed %q in %s (attempt %d/%d)", feed.URL, retryIn, retries, retryLimit)
					time.Sleep(retryIn)
					retries += 1
					continue
				}
				break
			}
		}()
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

	f.stats.Access(func(s *stats) {
		s.Duration = time.Since(s.StartTime)
		s.TotalFeeds = len(f.feeds)
		if s.SuccessFeeds > 0 {
			s.AvgFetchTime = s.TotalFetchTime / time.Duration(s.SuccessFeeds)
		}
		s.MemoryUsage = m.Alloc
	})

	f.state.Access(f.cleanState)

	if f.dry {
		f.logf("Fetched feeds: %d.\nAll feeds: %d.", fetchedFeeds.Load(), len(f.feeds))
		f.logf("Not reporting stats or saving state.")
		return nil
	}

	if f.serviceAccountKey != nil && f.statsSpreadsheetID != "" {
		f.stats.Access(func(s *stats) {
			if err := f.uploadStatsToSheets(ctx, s); err != nil {
				f.logf("Failed to upload stats to Google Sheets: %v", err)
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
			f.dlogf("Removing state for non-existent feed %q.", url)
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
