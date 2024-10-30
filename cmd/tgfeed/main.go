// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// vim: foldmethod=marker

/*
Tgfeed fetches RSS feeds and sends new articles via Telegram.

# How it works?

tgfeed runs as a GitHub Actions workflow.

New articles are sent to a Telegram chat specified by the CHAT_ID environment
variable.

# Configuration

tgfeed loads it's configuration from config.star file on GitHub Gist. This file
is written in Starlark language and defines a list of feeds, for example:

	feeds = [
	    feed(
	        url = "https://hnrss.org/newest",
	        title = "Hacker News: Newest",
	        block_rule = lambda item: "pdf" in item.title.lower(), # Block PDF files.
	    )
	]

Each feed can have a title, URL, and optional block and keep rules.

Block and keep rules are Starlark functions that take a feed item as an argument
and return a boolean value. If a block rule returns true, the item is not sent
to Telegram. If a keep rule returns true, the item is sent to Telegram, even if
it doesn't match other criteria.

The feed item passed to block_rule and keep_rule is a dictionary with the
following keys:

  - title: The title of the item.
  - description: The description of the item.
  - content: The content of the item.
  - categories: A list of categories the item belongs to.

# Where it keeps state?

tgfeed stores it's state on GitHub Gist.

It maintains a state for each feed, including last modified time, last updated
time, ETag, error count, and last error message. It keeps track of failing feeds
and disables them after a certain threshold of consecutive failures. State
information is stored and updated in the state.json file on GitHub Gist. You
won't need to touch this file at all, except from very rare cases.

# Environment variables

The tgfeed program relies on the following environment variables:

  - CHAT_ID: Telegram chat ID where the program sends new articles.
  - GIST_ID: GitHub Gist ID where the program stores its state.
  - GITHUB_TOKEN: GitHub personal access token for accessing the GitHub API.
  - TELEGRAM_TOKEN: Telegram bot token for accessing the Telegram Bot API.
  - STATS_SPREADSHEET_ID: ID of the Google Spreadsheet to which the program uploads
    statistics for every run. This is required if the SERVICE_ACCOUNT_KEY is
    provided.
  - SERVICE_ACCOUNT_KEY: JSON string representing the service account key for
    accessing the Google API. It's not required, and stats won't be uploaded to a
    spreadsheet if this variable is not set.

# Stats collection

tgfeed collects and reports stats about every run to Google Sheets.
You can specify the ID of the spreadsheet via the STATS_SPREADSHEET_ID
environment variable. To collect stats, you must provide the SERVICE_ACCOUNT_KEY
environment variable with JSON string representing the service account key for
accessing the Google API. Stats include:

  - Total number of feeds fetched
  - Number of successfully fetched feeds
  - Number of feeds that failed to fetch
  - Number of feeds that were not modified
  - Start time of a run
  - Duration of a run
  - Number of parsed RSS items
  - Total fetch time
  - Average fetch time
  - Memory usage

You can use these stats to monitor performance of tgfeed and understand which
feeds are causing problems.

# Administration

To edit the config.star file, you can use the -edit flag. This will open the
file in your default editor (specified by the $EDITOR environment variable).
After you save the changes and close the editor, the updated config.star will
be saved back to the Gist. For example:

	$ tgfeed -edit

To reenable a failing feed that has been disabled due to consecutive failures,
you can use the -reenable flag followed by the URL of the feed. For example:

	$ tgfeed -reenable https://example.com/feed

To view the list of feeds, you can use the -feeds flag. This will also print the
URLs of feeds that have encountered errors during fetching. For example:

	$ tgfeed -feeds
*/
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
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

	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/api/google/serviceaccount"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/util/syncx"
	"go.astrophena.name/tools/internal/version"

	"github.com/mmcdole/gofeed"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

const (
	defaultErrorTemplate = `❌ Something went wrong:
<pre><code>%v</code></pre>`
	ghAPI            = "https://api.github.com"
	tgAPI            = "https://api.telegram.org"
	errorThreshold   = 12 // failing continuously for twelve days will disable feed and complain loudly
	concurrencyLimit = 10
)

// Some types of errors that can happen during tgfeed execution.
var (
	errAlreadyRunning = errors.New("already running")
	errNoFeed         = errors.New("no such feed")
)

func main() {
	cli.Run(func(ctx context.Context) error {
		return new(fetcher).main(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	})
}

func (f *fetcher) main(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdout, stderr io.Writer,
) error {
	// Check if this fetcher is already running.
	if f.running.Load() {
		return errAlreadyRunning
	}
	f.running.Store(true)
	defer f.running.Store(false)

	// Initialize logger.
	f.logf = log.New(stderr, "", 0).Printf

	// Define and parse flags.
	a := &cli.App{
		Name:        "tgfeed",
		Description: helpDoc,
		Credits:     credits,
		Flags:       flag.NewFlagSet("tgfeed", flag.ContinueOnError),
	}
	var (
		feeds    = a.Flags.Bool("feeds", false, "List available feeds.")
		edit     = a.Flags.Bool("edit", false, "Edit config.star file in your EDITOR.")
		reenable = a.Flags.String("reenable", "", "Reenable disabled `feed`.")
		run      = a.Flags.Bool("run", false, "Fetch feeds and send updates.")
	)
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	// Load configuration from environment variables.
	f.chatID = cmp.Or(f.chatID, getenv("CHAT_ID"))
	f.ghToken = cmp.Or(f.ghToken, getenv("GITHUB_TOKEN"))
	f.gistID = cmp.Or(f.gistID, getenv("GIST_ID"))
	f.statsSpreadsheetID = cmp.Or(f.statsSpreadsheetID, getenv("STATS_SPREADSHEET_ID"))
	f.tgToken = cmp.Or(f.tgToken, getenv("TELEGRAM_TOKEN"))

	// Load Google service account key from SERVICE_ACCOUNT_KEY environment
	// variable. If it's not defined, stats won't be uploaded to a Google
	// spreadsheet.
	if key := getenv("SERVICE_ACCOUNT_KEY"); key != "" {
		var err error
		f.serviceAccountKey, err = serviceaccount.LoadKey([]byte(key))
		if err != nil {
			return err
		}
	}

	// Initialize internal state.
	f.initOnce.Do(f.doInit)

	// Choose a mode based on passed flags and run it.
	switch {
	case *feeds:
		return f.listFeeds(ctx, stdout)
	case *edit:
		return f.edit(ctx)
	case *run:
		if err := f.run(ctx); err != nil {
			return f.errNotify(ctx, err)
		}
		return nil
	case *reenable != "":
		return f.reenable(ctx, *reenable)
	default:
		a.Flags.Usage()
		return fmt.Errorf("%w: pick a mode", cli.ErrInvalidArgs)
	}
}

type fetcher struct {
	running  atomic.Bool
	initOnce sync.Once

	fp       *gofeed.Parser
	httpc    *http.Client
	logf     logger.Logf
	scrubber *strings.Replacer

	gistID  string
	ghToken string
	gistc   *gist.Client

	config  string
	feeds   []*feed
	chatID  string
	tgToken string

	errorTemplate string

	stats              *stats
	serviceAccountKey  *serviceaccount.Key
	statsSpreadsheetID string

	mu    sync.Mutex
	state map[string]*feedState
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
	plural := "once"
	if n > 1 {
		plural = fmt.Sprintf("%d times", n)
	}
	return plural
}

// }}}

func (f *fetcher) edit(ctx context.Context) error { // {{{
	editor := os.Getenv("EDITOR")
	if editor == "" {
		return errors.New("$EDITOR is not defined")
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

	cmd := exec.Command(editor, tmpfile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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

	f.config = string(edited)
	return f.saveToGist(ctx)
} // }}}

func (f *fetcher) run(ctx context.Context) error { // {{{
	// Start with empty stats for every run.
	f.stats = &stats{
		StartTime: time.Now(),
	}

	if err := f.loadFromGist(ctx); err != nil {
		return fmt.Errorf("fetching gist failed: %w", err)
	}

	// Recreate updates channel on each fetch.
	updates := make(chan *gofeed.Item)

	// Start sending goroutine.
	go func() {
		for {
			select {
			case item, valid := <-updates:
				if !valid {
					return
				}
				f.sendUpdate(ctx, item)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Enqueue fetches.
	lwg := syncx.NewLimitedWaitGroup(concurrencyLimit)
	for _, feed := range shuffle(f.feeds) {
		lwg.Add(1)
		go func() {
			defer lwg.Done()
			f.fetch(ctx, feed, updates)
		}()
	}

	// Wait for all fetches to complete.
	lwg.Wait()
	// Stop sending goroutine.
	close(updates)

	// Prepare and report stats.

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	f.modifyStats(func(s *stats) {
		s.Duration = time.Since(s.StartTime)
		s.TotalFeeds = len(f.feeds)
		if s.SuccessFeeds > 0 {
			s.AvgFetchTime = s.TotalFetchTime / time.Duration(s.SuccessFeeds)
		}
		f.stats.MemoryUsage = m.Alloc
	})

	if f.serviceAccountKey != nil && f.statsSpreadsheetID != "" {
		if err := f.reportStats(ctx); err != nil {
			f.logf("Failed to report stats: %v", err)
		}
	}

	return f.saveToGist(ctx)
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
	URL       string
	Title     string
	BlockRule *starlark.Function // optional, (item) -> bool
	KeepRule  *starlark.Function // optional, (item) -> bool
}

// String implements the [starlark.Value] interface.
func (f *feed) String() string { return fmt.Sprintf("<feed title=%q url=%q>", f.Title, f.URL) }

// Type implements the [starlark.Value] interface.
func (f *feed) Type() string { return "feed" }

// Freeze implements the [starlark.Value] interface.
func (f *feed) Freeze() {} // immutable

// Truth implements the [starlark.Value] interface.
func (f *feed) Truth() starlark.Bool { return starlark.Bool(f.URL != "") }

// Hash implements the [starlark.Value] interface.
func (f *feed) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: %s", f.Type()) }

func feedFunc(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		title     string
		url       string
		blockRule *starlark.Function
		keepRule  *starlark.Function
	)

	if err := starlark.UnpackArgs("feed", args, kwargs,
		"url", &url,
		"title?", &title,
		"block_rule?", &blockRule,
		"keep_rule?", &keepRule,
	); err != nil {
		return nil, err
	}

	return &feed{
		BlockRule: blockRule,
		KeepRule:  keepRule,
		Title:     title,
		URL:       url,
	}, nil
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
	f.mu.Lock()
	defer f.mu.Unlock()
	state, exists = f.state[url]
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

	if err := f.parseConfig(f.config); err != nil {
		return err
	}

	f.state = make(map[string]*feedState)
	state, ok := g.Files["state.json"]
	if ok {
		return json.Unmarshal([]byte(state.Content), &f.state)
	}
	return nil
}

func (f *fetcher) parseConfig(config string) error {
	predecl := starlark.StringDict{
		"feed": starlark.NewBuiltin("feed", feedFunc),
	}

	globals, err := starlark.ExecFileOptions(
		&syntax.FileOptions{
			TopLevelControl: true,
		},
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.logf("%s", msg) },
		},
		"config.star",
		config,
		predecl,
	)
	if err != nil {
		return err
	}

	feedsList, ok := globals["feeds"].(*starlark.List)
	if !ok {
		return errors.New("feeds must be defined and be a list")
	}

	var feeds []*feed

	for elem := range feedsList.Elements() {
		feed, ok := elem.(*feed)
		if !ok {
			continue
		}
		feeds = append(feeds, feed)
	}

	f.feeds = feeds
	return nil
}

func (f *fetcher) saveToGist(ctx context.Context) error {
	state, err := json.MarshalIndent(f.state, "", "  ")
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
	mu sync.Mutex

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

func (f *fetcher) modifyStats(fn func(s *stats)) {
	f.stats.mu.Lock()
	defer f.stats.mu.Unlock()
	fn(f.stats)
}

func (f *fetcher) reportStats(ctx context.Context) error {
	f.stats.mu.Lock()
	defer f.stats.mu.Unlock()

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
		Range: "Stats",
		// https://developers.google.com/sheets/api/reference/rest/v4/Dimension
		MajorDimension: "ROWS",
		Values: [][]string{
			{
				fmt.Sprintf("%d", f.stats.TotalFeeds),
				fmt.Sprintf("%d", f.stats.SuccessFeeds),
				fmt.Sprintf("%d", f.stats.FailedFeeds),
				fmt.Sprintf("%d", f.stats.NotModifiedFeeds),
				f.stats.StartTime.Format(time.RFC3339),
				f.stats.Duration.String(),
				fmt.Sprintf("%d", f.stats.TotalItemsParsed),
				f.stats.TotalFetchTime.String(),
				f.stats.AvgFetchTime.String(),
				fmt.Sprintf("%d", f.stats.MemoryUsage),
			},
		},
	}

	_, err = request.Make[any](ctx, request.Params{
		Method: http.MethodPost,
		// https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption
		URL:  "https://sheets.googleapis.com/v4/spreadsheets/" + f.statsSpreadsheetID + "/values/Stats:append?valueInputOption=USER_ENTERED",
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
		f.mu.Lock()
		f.state[fd.URL] = new(feedState)
		state = f.state[fd.URL]
		f.mu.Unlock()
		state.LastUpdated = time.Now()
	}

	// Skip disabled feeds.
	if state.Disabled {
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

	res, err := f.httpc.Do(req)
	if err != nil {
		f.handleFetchFailure(ctx, state, fd.URL, err)
		return
	}
	defer res.Body.Close()

	// Ignore unmodified feeds and report an error otherwise.
	if res.StatusCode == http.StatusNotModified {
		f.modifyStats(func(s *stats) {
			s.NotModifiedFeeds += 1
		})
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
			if blocked, err := f.applyRule(fd.BlockRule, item); err != nil {
				f.logf("Error applying block rule for feed %q: %v", fd.URL, err)
				continue
			} else if blocked.(starlark.Bool) {
				continue
			}
		}

		if fd.KeepRule != nil {
			if keep, err := f.applyRule(fd.KeepRule, item); err != nil {
				f.logf("Error applying keep rule for feed %q: %v", fd.URL, err)
				continue
			} else if !keep.(starlark.Bool) {
				continue
			}
		}

		updates <- item
	}
	state.LastUpdated = time.Now()
	state.ErrorCount = 0
	state.LastError = ""
	state.FetchCount += 1

	f.modifyStats(func(s *stats) {
		s.TotalItemsParsed += len(feed.Items)
		s.SuccessFeeds += 1
		s.TotalFetchTime += time.Since(startTime)
	})
}

func (f *fetcher) applyRule(rule *starlark.Function, item *gofeed.Item) (starlark.Value, error) {
	var categories []starlark.Value
	for _, category := range item.Categories {
		categories = append(categories, starlark.String(category))
	}
	return starlark.Call(
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { f.logf("%s", msg) },
		},
		rule,
		starlark.Tuple{starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"title":       starlark.String(item.Title),
				"description": starlark.String(item.Description),
				"content":     starlark.String(item.Content),
				"categories":  starlark.NewList(categories),
			},
		)},
		[]starlark.Tuple{},
	)
}

func (f *fetcher) handleFetchFailure(ctx context.Context, state *feedState, url string, err error) {
	f.modifyStats(func(s *stats) {
		s.FailedFeeds += 1
	})

	state.FetchFailCount += 1
	state.ErrorCount += 1
	state.LastError = err.Error()

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
