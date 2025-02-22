// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// vim: foldmethod=marker

package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/cmd/tgfeed/internal/diff"
	"go.astrophena.name/tools/cmd/tgfeed/internal/ghnotify"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/api/google/serviceaccount"
	"go.astrophena.name/tools/internal/util/starlarkconv"

	"github.com/mmcdole/gofeed"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

const (
	defaultErrorTemplate = `❌ Something went wrong:
<pre><code>%v</code></pre>`
	ghAPI                 = "https://api.github.com"
	tgAPI                 = "https://api.telegram.org"
	errorThreshold        = 12 // failing continuously for N fetches will disable feed and complain loudly
	fetchConcurrencyLimit = 10 // N fetches that can run at the same time
	sendConcurrencyLimit  = 2  // N sends that can run at the same time
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
		return f.edit(ctx, env.Getenv, env.Stdin, env.Stdout, env.Stderr)
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

// Modes {{{

func (f *fetcher) listFeeds(ctx context.Context, w io.Writer) error { // {{{
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

// }}}

func (f *fetcher) edit( // {{{
	ctx context.Context,
	getenv func(string) string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) error {
	editor := getenv("EDITOR")
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
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
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
		if !f.ask("Do you want to save?", stdin) {
			return nil
		}

		_, err = f.parseConfig(string(edited))
		if err != nil {
			f.logf("Edited file is invalid: %v.", err)
			if f.ask("Do you want to try editing again?", stdin) {
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

// }}}

func (f *fetcher) run(ctx context.Context) error { // {{{
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
			f.fetch(ctx, feed, updates)
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
			if err := f.reportStats(ctx, s); err != nil {
				f.logf("Failed to report stats: %v", err)
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
} // }}}

func (f *fetcher) reenable(ctx context.Context, url string) error { // {{{
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
} // }}}

// }}}

// Feed state {{{

type feed struct {
	URL       string             `json:"url"`
	Title     string             `json:"title,omitempty"`
	BlockRule *starlark.Function `json:"-"`
	KeepRule  *starlark.Function `json:"-"`
}

func (f *feed) String() string        { return fmt.Sprintf("<feed url=%q>", f.URL) }
func (f *feed) Type() string          { return "feed" }
func (f *feed) Freeze()               {} // immutable
func (f *feed) Truth() starlark.Bool  { return starlark.Bool(f.URL != "") }
func (f *feed) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: %s", f.Type()) }

func feedBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("unexpected positional arguments")
	}
	f := new(feed)
	if err := starlark.UnpackArgs("feed", args, kwargs,
		"url", &f.URL,
		"title?", &f.Title,
		"block_rule?", &f.BlockRule,
		"keep_rule?", &f.KeepRule,
	); err != nil {
		return nil, err
	}
	return f, nil
}

type feedState struct {
	Disabled     bool      `json:"disabled"`
	LastUpdated  time.Time `json:"last_updated"`
	LastModified string    `json:"last_modified,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	ErrorCount   int       `json:"error_count,omitempty"`
	LastError    string    `json:"last_error,omitempty"`

	// Stats.
	FetchCount     int64 `json:"fetch_count"`      // successful fetches
	FetchFailCount int64 `json:"fetch_fail_count"` // failed fetches
}

func (f *fetcher) getState(url string) (state *feedState, exists bool) {
	f.state.RAccess(func(s map[string]*feedState) {
		state, exists = s[url]
	})
	return
}

func (f *fetcher) loadFromGist(ctx context.Context) error {
	g, err := f.gistc.Get(ctx, f.gistID)
	if err != nil {
		return err
	}

	errorTemplate, ok := g.Files["error.tmpl"]
	if ok {
		f.errorTemplate = errorTemplate.Content
	} else {
		f.errorTemplate = defaultErrorTemplate
	}

	config, ok := g.Files["config.star"]
	if !ok {
		return errors.New("config.star not found")
	}
	f.config = config.Content

	f.feeds, err = f.parseConfig(f.config)
	if err != nil {
		return err
	}

	stateMap := make(map[string]*feedState)
	state, ok := g.Files["state.json"]
	if ok {
		if err := json.Unmarshal([]byte(state.Content), &stateMap); err != nil {
			return err
		}
	}
	f.state = syncx.Protect(stateMap)

	return nil
}

func (f *fetcher) parseConfig(config string) ([]*feed, error) {
	globals, err := starlark.ExecFileOptions(
		&syntax.FileOptions{
			TopLevelControl: true,
		},
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.logf("%s", msg) },
		},
		"config.star",
		config,
		starlark.StringDict{
			"feed": starlark.NewBuiltin("feed", feedBuiltin),
		},
	)
	if err != nil {
		return nil, err
	}

	feedsList, ok := globals["feeds"].(*starlark.List)
	if !ok {
		return nil, errors.New("feeds must be defined and be a list")
	}

	var feeds []*feed

	for elem := range feedsList.Elements() {
		feed, ok := elem.(*feed)
		if !ok {
			continue
		}

		_, err := url.Parse(feed.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid URL %q of feed %q", feed.URL, feed.Title)
		}

		feeds = append(feeds, feed)
	}

	return feeds, nil
}

func (f *fetcher) saveToGist(ctx context.Context) error {
	var (
		state []byte
		err   error
	)
	f.state.RAccess(func(s map[string]*feedState) {
		state, err = json.MarshalIndent(s, "", "  ")
	})
	if err != nil {
		return err
	}
	ng := &gist.Gist{
		Files: map[string]gist.File{
			"config.star": {Content: f.config},
			"state.json":  {Content: string(state)},
		},
	}
	_, err = f.gistc.Update(ctx, f.gistID, ng)
	return err
}

// }}}

// Stats {{{

type stats struct {
	TotalFeeds       int `json:"total_feeds"`
	SuccessFeeds     int `json:"success_feeds"`
	FailedFeeds      int `json:"failed_feeds"`
	NotModifiedFeeds int `json:"not_modified_feeds"`

	StartTime        time.Time     `json:"start_time"`
	Duration         time.Duration `json:"duration"`
	TotalItemsParsed int           `json:"total_items_parsed"`

	TotalFetchTime time.Duration `json:"total_fetch_time"`
	AvgFetchTime   time.Duration `json:"avg_fetch_time"`

	MemoryUsage uint64 `json:"memory_usage"`
}

func (f *fetcher) reportStats(ctx context.Context, s *stats) error {
	sheetRange := f.statsSpreadsheetRange
	if sheetRange == "" {
		sheetRange = "Stats"
	}

	tok, err := f.serviceAccountKey.AccessToken(ctx, f.httpc, "https://www.googleapis.com/auth/spreadsheets")
	if err != nil {
		return err
	}

	// https://developers.google.com/sheets/api/reference/rest/v4/spreadsheets.values/append
	req := struct {
		Range          string     `json:"range"`
		MajorDimension string     `json:"majorDimension"`
		Values         [][]string `json:"values"`
	}{
		Range: sheetRange,
		// https://developers.google.com/sheets/api/reference/rest/v4/Dimension
		MajorDimension: "ROWS",
		Values: [][]string{
			{
				fmt.Sprintf("%d", s.TotalFeeds),
				fmt.Sprintf("%d", s.SuccessFeeds),
				fmt.Sprintf("%d", s.FailedFeeds),
				fmt.Sprintf("%d", s.NotModifiedFeeds),
				s.StartTime.Format(time.RFC3339),
				s.Duration.String(),
				fmt.Sprintf("%d", s.TotalItemsParsed),
				s.TotalFetchTime.String(),
				s.AvgFetchTime.String(),
				fmt.Sprintf("%d", s.MemoryUsage),
			},
		},
	}

	_, err = request.Make[any](ctx, request.Params{
		Method: http.MethodPost,
		// https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption
		URL:  "https://sheets.googleapis.com/v4/spreadsheets/" + f.statsSpreadsheetID + "/values/" + sheetRange + ":append?valueInputOption=USER_ENTERED",
		Body: req,
		Headers: map[string]string{
			"Authorization": "Bearer " + tok,
			"User-Agent":    version.UserAgent(),
		},
		HTTPClient: f.httpc,
		Scrubber:   f.scrubber,
	})
	return err
}

// }}}

// Feed fetching {{{

func (f *fetcher) fetch(ctx context.Context, fd *feed, updates chan *gofeed.Item) {
	startTime := time.Now()

	state, exists := f.getState(fd.URL)
	// If we don't remember this feed, it's probably new. Set it's last update
	// date to current so we don't get a lot of unread articles and trigger
	// Telegram Bot API rate limit.
	if !exists {
		f.dlogf("State for feed %q doesn't exist, creating it.", fd.URL)
		f.state.Access(func(s map[string]*feedState) {
			s[fd.URL] = new(feedState)
			state = s[fd.URL]
		})
		state.LastUpdated = time.Now()
	}

	// Skip disabled feeds.
	if state.Disabled {
		f.dlogf("Skipping disabled feed %q.", fd.URL)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fd.URL, nil)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.URL, err)
		return
	}

	req.Header.Set("User-Agent", version.UserAgent())
	if state.ETag != "" {
		req.Header.Set("If-None-Match", state.ETag)
	}
	if state.LastModified != "" {
		req.Header.Set("If-Modified-Since", state.LastModified)
	}

	var res *http.Response
	if fd.URL == "tgfeed://github-notifications" {
		h := ghnotify.Handler(f.ghToken, f.httpc)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		res = w.Result()
	} else {
		res, err = f.httpc.Do(req)
		if err != nil {
			f.handleFetchFailure(ctx, state, fd.URL, err)
			return
		}
	}

	defer res.Body.Close()

	// Ignore unmodified feeds and report an error otherwise.
	if res.StatusCode == http.StatusNotModified {
		f.dlogf("Feed %q was unmodified since last fetch.", fd.URL)
		f.stats.Access(func(s *stats) {
			s.NotModifiedFeeds += 1
		})
		state.LastUpdated = time.Now()
		state.ErrorCount = 0
		state.LastError = ""
		return
	}
	if res.StatusCode != http.StatusOK {
		const readLimit = 16384 // 16 KB is enough for error messages (probably)
		var body []byte
		body, err = io.ReadAll(io.LimitReader(res.Body, readLimit))
		if err != nil {
			body = []byte("unable to read body")
		}
		f.handleFetchFailure(ctx, state, fd.URL, fmt.Errorf("want 200, got %d: %s", res.StatusCode, body))
		return
	}

	feed, err := f.fp.Parse(res.Body)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.URL, err)
		return
	}

	state.ETag = res.Header.Get("ETag")
	if lastModified := res.Header.Get("Last-Modified"); lastModified != "" {
		state.LastModified = lastModified
	}

	for _, item := range feed.Items {
		if item.PublishedParsed.Before(state.LastUpdated) {
			continue
		}

		if fd.BlockRule != nil {
			if blocked := f.applyRule(fd.BlockRule, item); blocked {
				f.dlogf("Item %q was blocked due to block rule.", item.Link)
				continue
			}
		}

		if fd.KeepRule != nil {
			if keep := f.applyRule(fd.KeepRule, item); !keep {
				f.dlogf("Item %q was not kept due to keep rule.", item.Link)
				continue
			}
		}

		updates <- item
	}
	state.LastUpdated = time.Now()
	state.ErrorCount = 0
	state.LastError = ""
	state.FetchCount += 1

	f.stats.Access(func(s *stats) {
		s.TotalItemsParsed += len(feed.Items)
		s.SuccessFeeds += 1
		s.TotalFetchTime += time.Since(startTime)
	})
}

func (f *fetcher) applyRule(rule *starlark.Function, item *gofeed.Item) bool {
	var categories []starlark.Value
	for _, category := range item.Categories {
		categories = append(categories, starlark.String(category))
	}
	extensions, err := starlarkconv.ToValue(item.Extensions)
	if err != nil {
		f.logf("Error converting item extensions to Starlark value: %v", err)
		return false
	}
	val, err := starlark.Call(
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.logf("%s", msg) },
		},
		rule,
		starlark.Tuple{starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"title":       starlark.String(item.Title),
				"url":         starlark.String(item.Link),
				"description": starlark.String(item.Description),
				"content":     starlark.String(item.Content),
				"categories":  starlark.NewList(categories),
				"extensions":  extensions,
			},
		)},
		[]starlark.Tuple{},
	)
	if err != nil {
		f.logf("Error applying rule for item %q: %v", item.Link, err)
		return false
	}

	ret, ok := val.(starlark.Bool)
	if !ok {
		f.logf("Rule for item %q returned not a boolean value.", item.Link)
		return false
	}
	return bool(ret)
}

func (f *fetcher) handleFetchFailure(ctx context.Context, state *feedState, url string, err error) {
	f.stats.Access(func(s *stats) {
		s.FailedFeeds += 1
	})

	state.FetchFailCount += 1
	state.ErrorCount += 1
	state.LastError = err.Error()

	f.dlogf("Feed %q failed with an error: %v", url, err)

	// Complain loudly and disable feed, if we failed previously enough.
	if state.ErrorCount >= errorThreshold {
		err = fmt.Errorf("fetching feed %q failed after %d previous attempts: %v; feed was disabled, to reenable it run 'tgfeed -reenable %q'", url, state.ErrorCount, err, url)
		state.Disabled = true
		if err := f.errNotify(ctx, err); err != nil {
			f.logf("Notifying about a disabled feed failed: %v", err)
		}
	}
}

func (f *fetcher) sendUpdate(ctx context.Context, item *gofeed.Item) {
	title := item.Title
	if item.Title == "" {
		title = item.Link
	}

	msg := fmt.Sprintf(
		`🔗 <a href="%[1]s">%[2]s</a>`,
		item.Link,
		html.EscapeString(title),
	)

	inlineKeyboardButtons := []inlineKeyboardButton{}

	// hnrss.org feeds have Hacker News entry URL set as GUID. Also send it
	// because I often read comments on Hacker News entries.
	if strings.HasPrefix(item.GUID, "https://news.ycombinator.com/item?id=") {
		inlineKeyboardButtons = append(inlineKeyboardButtons, inlineKeyboardButton{
			Text: "↪ Hacker News",
			URL:  item.GUID,
		})
	}

	// If in dry mode, simply log the message, but don't send it.
	if f.dry {
		f.logf("Will send message:\n\t%s\n", msg)
		return
	}

	if err := f.send(ctx, strings.TrimSpace(msg), func(args map[string]any) {
		args["reply_markup"] = map[string]any{
			"inline_keyboard": [][]inlineKeyboardButton{inlineKeyboardButtons},
		}
	}); err != nil {
		f.logf("Sending %q to %q failed: %v", msg, f.chatID, err)
	}
}

// }}}

// Telegram message sending {{{

func (f *fetcher) send(ctx context.Context, message string, modify func(args map[string]any)) error {
	args := map[string]any{
		"chat_id":    f.chatID,
		"parse_mode": "HTML",
		"text":       message,
	}
	if modify != nil {
		modify(args)
	}
	return f.makeTelegramRequest(ctx, "sendMessage", args)
}

func (f *fetcher) errNotify(ctx context.Context, err error) error {
	return f.send(ctx, fmt.Sprintf(f.errorTemplate, html.EscapeString(err.Error())), disableLinkPreview)
}

// https://core.telegram.org/bots/api#linkpreviewoptions
type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

func disableLinkPreview(args map[string]any) {
	args["link_preview_options"] = linkPreviewOptions{
		IsDisabled: true,
	}
}

// https://core.telegram.org/bots/api#inlinekeyboardbutton
type inlineKeyboardButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

func (f *fetcher) makeTelegramRequest(ctx context.Context, method string, args any) error {
	if _, err := request.Make[any](ctx, request.Params{
		Method: http.MethodPost,
		URL:    tgAPI + "/bot" + f.tgToken + "/" + method,
		Body:   args,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		HTTPClient: f.httpc,
		Scrubber:   f.scrubber,
	}); err != nil {
		return err
	}
	return nil
}

// }}}
