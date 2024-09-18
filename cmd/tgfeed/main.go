// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// vim: foldmethod=marker

/*
Tgfeed fetches RSS feeds and sends new articles via Telegram.

# How it works?

tgfeed runs as a GitHub Actions workflow.

It fetches RSS feeds from URLs provided in the feeds.json file on GitHub Gist
that is a simple array of feed URLs:

	[
	  "https://astrophena.name/feed.xml"
	]

New articles are sent to a Telegram chat specified by the CHAT_ID environment
variable.

# Where it keeps state?

tgfeed stores it's state on GitHub Gist, optionally encrypted with the help of
age encryption library (https://filippo.io/age).

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
  - STATE_PASSWORD: Optional password with which the state is encrypted.
  - SERVICE_ACCOUNT_KEY: JSON string representing the service account key for
    accessing the Google API. It's not required, and stats won't be uploaded to a
    spreadsheet if this variable is not set.

# Summarization with Gemini API

tgfeed can summarize the text content of articles using the Gemini API. This
feature requires setting the GEMINI_API_KEY environment variable. When provided,
tgfeed will attempt to summarize the description field of fetched RSS items and
include the summary in the Telegram notification along with the article title
and link.

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

To subscribe to a feed, you can use the -subscribe flag followed by the URL of
the feed. For example:

	$ tgfeed -subscribe https://example.com/feed

To unsubscribe from a feed, you can use the -unsubscribe flag followed by the URL of
the feed. For example:

	$ tgfeed -unsubscribe https://example.com/feed

To reenable a failing feed that has been disabled due to consecutive failures,
you can use the -reenable flag followed by the URL of the feed. For example:

	$ tgfeed -reenable https://example.com/feed

To view the list of feeds, you can use the -feeds flag. This will also print the
URLs of feeds that have encountered errors during fetching. For example:

	$ tgfeed -feeds
*/
package main

import (
	"bytes"
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
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/api/google/gemini"
	"go.astrophena.name/tools/internal/api/google/serviceaccount"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/util/syncx"
	"go.astrophena.name/tools/internal/version"

	"filippo.io/age"
	"filippo.io/age/armor"
	"github.com/mmcdole/gofeed"
)

const (
	defaultErrorTemplate = `❌ Something went wrong:
<pre><code>%v</code></pre>`
	ghAPI            = "https://api.github.com"
	tgAPI            = "https://api.telegram.org"
	errorThreshold   = 12 // failing continuously for four days will disable feed and complain loudly
	concurrencyLimit = 10
)

// Some types of errors that can happen during tgfeed execution.
var (
	errUnknownMode    = errors.New("unknown mode")
	errAlreadyRunning = errors.New("already running")
	errDuplicateFeed  = errors.New("already in list of feeds")
	errNoFeed         = errors.New("no such feed")
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	f := new(fetcher)
	cli.Run(f.main(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
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
		Flags:       flag.NewFlagSet("tgfeed", flag.ContinueOnError),
	}
	var (
		feeds       = a.Flags.Bool("feeds", false, "List available feeds.")
		reenable    = a.Flags.String("reenable", "", "Reenable disabled `feed`.")
		run         = a.Flags.Bool("run", false, "Fetch feeds and send updates.")
		subscribe   = a.Flags.String("subscribe", "", "Subscribe to a `feed`.")
		unsubscribe = a.Flags.String("unsubscribe", "", "Unsubscribe from a `feed`.")
	)
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	// Load configuration from environment variables.
	f.chatID = cmp.Or(f.chatID, getenv("CHAT_ID"))
	f.geminiKey = cmp.Or(f.geminiKey, getenv("GEMINI_API_KEY"))
	f.ghToken = cmp.Or(f.ghToken, getenv("GITHUB_TOKEN"))
	f.gistID = cmp.Or(f.gistID, getenv("GIST_ID"))
	f.statsSpreadsheetID = cmp.Or(f.statsSpreadsheetID, getenv("STATS_SPREADSHEET_ID"))
	f.tgToken = cmp.Or(f.tgToken, getenv("TELEGRAM_TOKEN"))

	// Initialize encryption key.
	if password := getenv("STATE_PASSWORD"); password != "" {
		var err error
		f.stateIdent, err = age.NewScryptIdentity(password)
		if err != nil {
			return err
		}
		f.stateRecipient, err = age.NewScryptRecipient(password)
		if err != nil {
			return err
		}
		f.encrypted = true
	}

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
	case *run:
		if err := f.run(ctx); err != nil {
			return f.errNotify(ctx, err)
		}
		return nil
	case *subscribe != "":
		return f.subscribe(ctx, *subscribe)
	case *reenable != "":
		return f.reenable(ctx, *reenable)
	case *unsubscribe != "":
		return f.unsubscribe(ctx, *unsubscribe)
	default:
		a.Flags.Usage()
		return cli.ErrArgsNeeded
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

	encrypted      bool
	stateIdent     *age.ScryptIdentity
	stateRecipient *age.ScryptRecipient

	geminiKey string
	geminic   *gemini.Client

	feeds   []string
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

	if f.geminiKey != "" {
		f.geminic = &gemini.Client{
			APIKey:     f.geminiKey,
			Model:      "gemini-1.5-flash-latest",
			HTTPClient: f.httpc,
			Scrubber:   f.scrubber,
		}
	}

	f.fp = gofeed.NewParser()

	scrubPairs := []string{
		f.ghToken, "[EXPUNGED]",
		f.tgToken, "[EXPUNGED]",
	}
	if f.geminiKey != "" {
		scrubPairs = append(scrubPairs, f.geminiKey, "[EXPUNGED]")
	}
	// Quick sanity check.
	if len(scrubPairs)%2 != 0 {
		panic("scrubPairs are not even; check doInit method on fetcher")
	}

	f.scrubber = strings.NewReplacer(scrubPairs...)

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

	for _, url := range f.feeds {
		state, hasState := f.getState(url)
		fmt.Fprintf(&sb, "%s", url)
		if !hasState {
			fmt.Fprintf(&sb, " \n")
			continue
		}
		fmt.Fprintf(&sb, " (")
		if state.CachedTitle != "" {
			fmt.Fprintf(&sb, "%q, ", state.CachedTitle)
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
	for _, url := range shuffle(f.feeds) {
		lwg.Add(1)
		go func(url string) {
			defer lwg.Done()
			f.fetch(ctx, url, updates)
		}(url)
	}

	// Wait for all fetches to complete.
	lwg.Wait()
	// Stop sending goroutine.
	close(updates)

	slices.Sort(f.feeds)
	if err := f.saveToGist(ctx); err != nil {
		return err
	}

	// Prepare and report stats.
	f.stats.mu.Lock()
	defer f.stats.mu.Unlock()

	f.stats.Duration = time.Since(f.stats.StartTime)
	f.stats.TotalFeeds = len(f.feeds)
	if f.stats.SuccessFeeds > 0 {
		f.stats.AvgFetchTime = f.stats.TotalFetchTime / time.Duration(f.stats.SuccessFeeds)
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	f.stats.MemoryUsage = m.Alloc

	if f.serviceAccountKey != nil && f.statsSpreadsheetID != "" {
		if err := f.reportStats(ctx); err != nil {
			f.logf("Failed to report stats: %v", err)
		}
	}

	return nil
}

func shuffle[S any](s []S) []S {
	s2 := slices.Clone(s)
	rand.Shuffle(len(s2), func(i, j int) {
		s2[i], s2[j] = s2[j], s2[i]
	})
	return s2
} // }}}

func (f *fetcher) subscribe(ctx context.Context, url string) error { // {{{
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}
	if slices.Contains(f.feeds, url) {
		return fmt.Errorf("%q: %w", url, errDuplicateFeed)
	}
	f.feeds = append(f.feeds, url)
	return f.saveToGist(ctx)
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

func (f *fetcher) unsubscribe(ctx context.Context, url string) error { // {{{
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}
	if !slices.Contains(f.feeds, url) {
		return fmt.Errorf("%q: %w", url, errNoFeed)
	}
	f.feeds = slices.DeleteFunc(f.feeds, func(sub string) bool {
		return sub == url
	})
	delete(f.state, url)
	return f.saveToGist(ctx)
} // }}}

// }}}

// Feed state {{{

type feedState struct {
	Disabled     bool      `json:"disabled"`
	LastUpdated  time.Time `json:"last_updated"`
	LastModified time.Time `json:"last_modified,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	ErrorCount   int       `json:"error_count,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	CachedTitle  string    `json:"cached_title,omitempty"`

	// Stats.
	FetchCount     int64 `json:"fetch_count"`      // successful fetches
	FetchFailCount int64 `json:"fetch_fail_count"` // failed fetches

	// Special flags. Not covered by tests or any common sense.

	// Only return updates matching this list of categories.
	FilteredCategories []string `json:"filtered_categories,omitempty"`
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
		b, err := decrypt([]byte(errorTemplate.Content), f.stateIdent)
		if err != nil {
			return err
		}
		f.errorTemplate = string(b)
	} else {
		f.errorTemplate = defaultErrorTemplate
	}

	feeds, ok := g.Files["feeds.json"]
	if ok {
		b, err := decrypt([]byte(feeds.Content), f.stateIdent)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &f.feeds); err != nil {
			return err
		}
	}

	f.state = make(map[string]*feedState)
	state, ok := g.Files["state.json"]
	if ok {
		b, err := decrypt([]byte(state.Content), f.stateIdent)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, &f.state)
	}
	return nil
}

func decrypt(b []byte, ident age.Identity) ([]byte, error) {
	if ident == nil || !isEncrypted(b) {
		return b, nil
	}
	cr, err := age.Decrypt(armor.NewReader(bytes.NewReader(b)), ident)
	if err != nil {
		return nil, err
	}
	cb, err := io.ReadAll(cr)
	if err != nil {
		return nil, err
	}
	return cb, nil
}

func isEncrypted(b []byte) bool {
	return bytes.HasPrefix(b, []byte("-----BEGIN AGE ENCRYPTED FILE-----\n"))
}

func encrypt(b []byte, recipient age.Recipient) ([]byte, error) {
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	cw, err := age.Encrypt(aw, recipient)
	if err != nil {
		return nil, err
	}
	if _, err = cw.Write(b); err != nil {
		return nil, err
	}
	if err := cw.Close(); err != nil {
		return nil, err
	}
	if err := aw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (f *fetcher) marshalAndOrEncrypt(v any) ([]byte, error) {
	j, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	if !f.encrypted {
		return j, nil
	}
	return encrypt(j, f.stateRecipient)
}

func (f *fetcher) saveToGist(ctx context.Context) error {
	state, err := f.marshalAndOrEncrypt(f.state)
	if err != nil {
		return err
	}
	feeds, err := f.marshalAndOrEncrypt(f.feeds)
	if err != nil {
		return err
	}
	ng := &gist.Gist{
		Files: map[string]gist.File{
			"feeds.json": {Content: string(feeds)},
			"state.json": {Content: string(state)},
		},
	}
	_, err = f.gistc.Update(ctx, f.gistID, ng)
	return err
}

// }}}

// Stats {{{

// stats represents data uploaded at every run to stats collector.
//
// DON'T CHANGE LAYOUT OF THIS STRUCT!!!
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

func (f *fetcher) reportStats(ctx context.Context) error {
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
	})
	return err
}

// }}}

// Feed fetching {{{

func (f *fetcher) fetch(ctx context.Context, url string, updates chan *gofeed.Item) {
	startTime := time.Now()

	state, exists := f.getState(url)
	// If we don't remember this feed, it's probably new. Set it's last update
	// date to current so we don't get a lot of unread articles and trigger
	// Telegram Bot API rate limit.
	if !exists {
		f.mu.Lock()
		f.state[url] = new(feedState)
		state = f.state[url]
		f.mu.Unlock()
		state.LastUpdated = time.Now()
	}

	// Skip disabled feeds.
	if state.Disabled {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		f.handleFetchFailure(ctx, state, url, err)
		return
	}

	req.Header.Set("User-Agent", version.UserAgent())
	if state.ETag != "" {
		req.Header.Set("If-None-Match", fmt.Sprintf(`"%s"`, state.ETag))
	}
	if !state.LastModified.IsZero() {
		req.Header.Set("If-Modified-Since", state.LastModified.In(time.UTC).Format(time.RFC1123))
	}

	res, err := f.httpc.Do(req)
	if err != nil {
		f.handleFetchFailure(ctx, state, url, err)
		return
	}
	defer res.Body.Close()

	// Ignore unmodified feeds and report an error otherwise.
	if res.StatusCode == http.StatusNotModified {
		f.stats.mu.Lock()
		defer f.stats.mu.Unlock()
		f.stats.NotModifiedFeeds += 1
		return
	}
	if res.StatusCode != http.StatusOK {
		const readLimit = 16384 // 16 KB is enough for error messages (probably)
		var body []byte
		body, err = io.ReadAll(io.LimitReader(res.Body, readLimit))
		if err != nil {
			body = []byte("unable to read body")
		}
		f.handleFetchFailure(ctx, state, url, fmt.Errorf("want 200, got %d: %s", res.StatusCode, body))
		return
	}

	feed, err := f.fp.Parse(res.Body)
	if err != nil {
		f.handleFetchFailure(ctx, state, url, err)
		return
	}

	state.ETag = res.Header.Get("ETag")
	if lastModified := res.Header.Get("Last-Modified"); lastModified != "" {
		parsed, err := time.ParseInLocation(time.RFC1123, lastModified, time.UTC)
		if err == nil {
			state.LastModified = parsed
		}
	}

	for _, item := range feed.Items {
		if item.PublishedParsed.Before(state.LastUpdated) {
			continue
		}
		if len(state.FilteredCategories) > 0 {
			if !slices.ContainsFunc(item.Categories, func(category string) bool {
				return slices.Contains(state.FilteredCategories, category)
			}) {
				continue
			}
		}
		// Skip some ads in Telegram channels.
		if strings.Contains(item.Description, "#реклама") {
			continue
		}
		updates <- item
	}
	state.LastUpdated = time.Now()
	state.ErrorCount = 0
	state.LastError = ""
	state.CachedTitle = feed.Title
	state.FetchCount += 1

	f.stats.mu.Lock()
	defer f.stats.mu.Unlock()
	f.stats.TotalItemsParsed += len(feed.Items)
	f.stats.SuccessFeeds += 1
	f.stats.TotalFetchTime += time.Since(startTime)
}

func (f *fetcher) handleFetchFailure(ctx context.Context, state *feedState, url string, err error) {
	f.stats.mu.Lock()
	f.stats.FailedFeeds += 1
	f.stats.mu.Unlock()

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

var bannedDomains = []string{
	// Twitter/X. Not available without login.
	"twitter.com",
	"x.com",
	// Reddit. Shithole.
	"old.reddit.com",
	"reddit.com",
}

func (f *fetcher) sendUpdate(ctx context.Context, item *gofeed.Item) {
	title := item.Title
	if item.Title == "" {
		title = item.Link
	}

	// Filter links from banned domains.
	u, err := url.Parse(item.Link)
	if err != nil {
		f.logf("sendUpdate: item %q has an invalid URL, skipping", item.Link)
		return
	}
	if slices.Contains(bannedDomains, u.Host) {
		f.logf("sendUpdate: skipping item %q from banned domain", item.Link)
		return
	}

	msg := fmt.Sprintf(
		`🔗 <a href="%[1]s">%[2]s</a>`,
		item.Link,
		// Escape HTML from title, but replace non-breaking spaces with normal ones.
		strings.Replace(html.EscapeString(title), "&nbsp;", " ", -1),
	)

	// If we have access to Gemini API, try to summarize an article.
	if f.geminic != nil && item.Description != "" {
		summary, err := f.summarize(ctx, item.Description)
		if err != nil {
			f.logf("sendUpdate: summarizing item %q failed: %v", item.Link, err)
		}
		if summary != "" && !strings.Contains(summary, "TGFEED_SKIP") {
			msg += "\n<blockquote>" + html.EscapeString(summary) + "</blockquote>"
		}
	}

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

func (f *fetcher) summarize(ctx context.Context, text string) (string, error) {
	const systemInstruction = `
You are a friendly bot that fetches articles from RSS feeds and given
descriptions of articles, YouTube videos and sometimes full articles themselves.

Your task is to make a concise summary of article or video description in three
sentences in English.

If text only contains an image or something you can't summarize, return exactly
"TGFEED_SKIP" (without quotes).
`

	params := gemini.GenerateContentParams{
		Contents: []*gemini.Content{
			{
				Parts: []*gemini.Part{{Text: text}},
			},
		},
		SystemInstruction: &gemini.Content{
			Parts: []*gemini.Part{{Text: systemInstruction}},
		},
	}

	resp, err := f.geminic.GenerateContent(ctx, params)
	if err != nil {
		return "", err
	}
	if len(resp.Candidates) == 0 {
		return "", errors.New("no candidates provided")
	}
	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return "", errors.New("candidate.Content is nil or has no Parts")
	}
	return candidate.Content.Parts[0].Text, nil
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
	}); err != nil {
		return err
	}
	return nil
}

// }}}
