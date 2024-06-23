/*
Tgfeed fetches RSS feeds and sends new articles via Telegram.

# How it works?

tgfeed runs as a GitHub Actions workflow.

It fetches RSS feeds from URLs provided in the feeds.json file on GitHub Gist.

New articles are sent to a Telegram chat specified by the CHAT_ID environment
variable.

# Where it keeps state?

tgfeed stores it's state on GitHub Gist.

It maintains a state for each feed, including last modified time, last
updated time, ETag, error count, and last error message. It keeps track of
failing feeds and disables them after a certain threshold of consecutive
failures. State information is stored and updated in the state.json file on
GitHub Gist.

# Environment variables

The tgfeed program relies on the following environment variables:

  - CHAT_ID: Telegram chat ID where the program sends new articles.
  - GIST_ID: GitHub Gist ID where the program stores its state.
  - GITHUB_TOKEN: GitHub personal access token for accessing the GitHub API.
  - TELEGRAM_TOKEN: Telegram bot token for accessing the Telegram Bot API.

# Summarization with Gemini API

tgfeed can summarize the text content of articles using the Gemini API. This
feature requires setting the GEMINI_API_KEY environment variable. When provided,
tgfeed will attempt to summarize the description field of fetched RSS items and
include the summary in the Telegram notification along with the article title
and link.

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
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/client/gemini"
	"go.astrophena.name/tools/internal/client/gist"
	"go.astrophena.name/tools/internal/httplogger"
	"go.astrophena.name/tools/internal/httputil"
	"go.astrophena.name/tools/internal/syncutil"

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

func main() {
	var (
		feeds       = flag.Bool("feeds", false, "List subscribed feeds.")
		reenable    = flag.String("reenable", "", "Reenable previously failing and disabled `feed`.")
		run         = flag.Bool("run", false, "Fetch feeds and send updates.")
		subscribe   = flag.String("subscribe", "", "Subscribe to a `feed`.")
		unsubscribe = flag.String("unsubscribe", "", "Unsubscribe from a `feed`.")
	)
	cli.SetArgsUsage("[flags]")
	cli.HandleStartup()

	f := &fetcher{
		httpc: &http.Client{
			Timeout: 10 * time.Second,
		},
		gistID:              os.Getenv("GIST_ID"),
		ghToken:             os.Getenv("GITHUB_TOKEN"),
		chatID:              os.Getenv("CHAT_ID"),
		tgToken:             os.Getenv("TELEGRAM_TOKEN"),
		statsCollectorURL:   os.Getenv("STATS_COLLECTOR_URL"),
		statsCollectorToken: os.Getenv("STATS_COLLECTOR_TOKEN"),
	}
	f.initOnce.Do(f.doInit)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey != "" {
		f.geminic = &gemini.Client{
			APIKey:     geminiKey,
			Model:      "gemini-1.5-flash-latest",
			HTTPClient: f.httpc,
		}
	}

	if os.Getenv("HTTPLOG") == "1" {
		if f.httpc.Transport == nil {
			f.httpc.Transport = http.DefaultTransport
		}
		f.httpc.Transport = httplogger.New(f.httpc.Transport, nil)
	}

	switch {
	case *feeds:
		if err := f.listFeeds(ctx, os.Stdout); err != nil {
			log.Fatal(err)
		}
	case *reenable != "":
		if err := f.reenable(ctx, *reenable); err != nil {
			log.Fatal(err)
		}
	case *run:
		if err := f.run(ctx); err != nil {
			if err := f.send(ctx, fmt.Sprintf(f.errorTemplate, html.EscapeString(err.Error())), disableLinkPreview); err != nil {
				log.Printf("notifying about an error failed: %v", err)
			}
			log.Fatal(err)
		}
	case *subscribe != "":
		if err := f.subscribe(ctx, *subscribe); err != nil {
			log.Fatal(err)
		}
	case *unsubscribe != "":
		if err := f.unsubscribe(ctx, *unsubscribe); err != nil {
			log.Fatal(err)
		}
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func (f *fetcher) reenable(ctx context.Context, url string) error {
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}

	state, ok := f.getState(url)
	if !ok {
		return fmt.Errorf("feed %q doesn't exist in state", url)
	}

	state.Disabled = false
	state.ErrorCount = 0
	state.LastError = ""

	return f.saveToGist(ctx)
}

func (f *fetcher) listFeeds(ctx context.Context, w io.Writer) error {
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
			failCount := "once"
			if state.ErrorCount > 1 {
				failCount = fmt.Sprintf("%d times", state.ErrorCount)
			}
			fmt.Fprintf(&sb, ", failed %s, last error was %q", failCount, state.LastError)
		}
		if state.Disabled {
			fmt.Fprintf(&sb, ", disabled")
		}
		fmt.Fprintf(&sb, ")\n")
	}

	io.WriteString(w, sb.String())
	return nil
}

func (f *fetcher) subscribe(ctx context.Context, url string) error {
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}
	if slices.Contains(f.feeds, url) {
		return fmt.Errorf("%q is already in list of feeds", url)
	}
	f.feeds = append(f.feeds, url)
	return f.saveToGist(ctx)
}

func (f *fetcher) unsubscribe(ctx context.Context, url string) error {
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}
	if !slices.Contains(f.feeds, url) {
		return fmt.Errorf("%q isn't in list of feeds", url)
	}
	f.feeds = slices.DeleteFunc(f.feeds, func(sub string) bool {
		return sub == url
	})
	delete(f.state, url)
	return f.saveToGist(ctx)
}

type fetcher struct {
	initOnce sync.Once

	httpc *http.Client
	fp    *gofeed.Parser

	gistID  string
	ghToken string
	gistc   *gist.Client
	geminic *gemini.Client

	feeds   []string
	chatID  string
	tgToken string

	errorTemplate string

	stats               *stats
	statsCollectorURL   string
	statsCollectorToken string

	mu    sync.Mutex
	state map[string]*feedState
}

type feedState struct {
	LastModified time.Time `json:"last_modified"`
	LastUpdated  time.Time `json:"last_updated"`
	ETag         string    `json:"etag"`
	Disabled     bool      `json:"disabled"`
	ErrorCount   int       `json:"error_count"`
	LastError    string    `json:"last_error"`
	CachedTitle  string    `json:"cached_title"`

	// Stats.
	FetchCount     int64 `json:"fetch_count"`      // successful fetches
	FetchFailCount int64 `json:"fetch_fail_count"` // failed fetches

	// Special flags. Not covered by tests or any common sense.

	// Only return updates matching this list of categories.
	FilteredCategories []string `json:"filtered_categories"`
}

type stats struct {
	mu sync.Mutex

	Time                  time.Time     `json:"time"`                     // date and time of run
	Duration              time.Duration `json:"duration"`                 // time consumed by run execution
	FeedsCount            int           `json:"feeds_count"`              // count of fetched feeds
	FeedsSuccessfulCount  int           `json:"feeds_successful_count"`   // count of successfully fetched feeds
	FeedsFailedCount      int           `json:"feeds_failed_count"`       // count of unsuccessfully fetched feeds
	FeedsNotModifiedCount int           `json:"feeds_not_modified_count"` // count of feeds that responded with Not Modified
}

func (f *fetcher) reportStats(ctx context.Context) error {
	f.stats.mu.Lock()
	defer f.stats.mu.Unlock()

	type response struct {
		Status  string `json:"status"`
		Message string `json:"message,omitempty"`
	}

	u, err := url.Parse(f.statsCollectorURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Add("token", f.statsCollectorToken)
	u.RawQuery = q.Encode()

	resp, err := httputil.MakeJSONRequest[response](ctx, httputil.RequestParams{
		Method: http.MethodPost,
		URL:    u.String(),
		Body:   []*stats{f.stats},
	})
	if err != nil {
		return err
	}

	if resp.Status == "error" {
		return fmt.Errorf("got server error: %v", resp.Message)
	}
	return nil
}

func (f *fetcher) doInit() {
	f.fp = gofeed.NewParser()
	f.fp.UserAgent = httputil.UserAgent()
	f.fp.Client = f.httpc

	f.gistc = &gist.Client{
		Token:      f.ghToken,
		HTTPClient: f.httpc,
	}
}

func (f *fetcher) run(ctx context.Context) error {
	f.initOnce.Do(f.doInit)

	// Start with empty stats for every run.
	f.stats = &stats{
		Time: time.Now(),
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
	lwg := syncutil.NewLimitedWaitGroup(concurrencyLimit)
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

	// Easter egg, for god's sake!
	now := time.Now()
	if !testing.Testing() && now.Month() == time.June && now.Day() == 10 && now.Hour() < 8 {
		f.makeTelegramRequest(ctx, "sendVideo", map[string]any{
			"chat_id": f.chatID,
			"video":   "https://astrophena.name/lol.mp4",
			"caption": "🎂",
		})
	}

	slices.Sort(f.feeds)
	if err := f.saveToGist(ctx); err != nil {
		return err
	}

	// Record time consumed by run and report stats.
	f.stats.mu.Lock()
	defer f.stats.mu.Unlock()
	f.stats.Duration = time.Now().Sub(f.stats.Time)
	f.stats.FeedsCount = len(f.feeds)

	if f.statsCollectorURL != "" && f.statsCollectorToken != "" {
		if err := f.reportStats(ctx); err != nil {
			log.Printf("failed to report stats: %v", err)
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

	// If we have access to Gemini API, try to summarize an article.
	if f.geminic != nil && item.Description != "" {
		summary, err := f.summarize(ctx, item.Description)
		if err != nil {
			log.Printf("sendUpdate: summarizing item %q failed: %v", item.Link, err)
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
			Text: "💬 Comments",
			URL:  item.GUID,
		})
	}

	if err := f.send(ctx, strings.TrimSpace(msg), func(args map[string]any) {
		args["reply_markup"] = map[string]any{
			"inline_keyboard": [][]inlineKeyboardButton{inlineKeyboardButtons},
		}
	}); err != nil {
		log.Printf("sending %q to %q failed: %v", msg, f.chatID, err)
	}
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

	feeds, ok := g.Files["feeds.json"]
	if ok {
		if err := json.Unmarshal([]byte(feeds.Content), &f.feeds); err != nil {
			return err
		}
	}

	f.state = make(map[string]*feedState)
	state, ok := g.Files["state.json"]
	if ok {
		return json.Unmarshal([]byte(state.Content), &f.state)
	}
	return nil
}

func (f *fetcher) fetch(ctx context.Context, url string, updates chan *gofeed.Item) {
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

	req.Header.Set("User-Agent", httputil.UserAgent())
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
		f.stats.FeedsNotModifiedCount += 1
		return
	}
	if res.StatusCode != http.StatusOK {
		f.handleFetchFailure(ctx, state, url, fmt.Errorf("want 200, got %d", res.StatusCode))
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
	f.stats.FeedsSuccessfulCount += 1
	f.stats.mu.Unlock()
}

func (f *fetcher) handleFetchFailure(ctx context.Context, state *feedState, url string, err error) {
	f.stats.mu.Lock()
	f.stats.FeedsFailedCount += 1
	f.stats.mu.Unlock()

	state.FetchFailCount += 1
	state.ErrorCount += 1
	state.LastError = err.Error()

	// Complain loudly and disable feed, if we failed previously enough.
	if state.ErrorCount >= errorThreshold {
		err = fmt.Errorf("fetching feed %q failed after %d previous attempts: %v; feed was disabled, to reenable it run 'tgfeed -reenable %q'", url, state.ErrorCount, err, url)
		state.Disabled = true
		if err := f.send(ctx, fmt.Sprintf(f.errorTemplate, html.EscapeString(err.Error())), disableLinkPreview); err != nil {
			log.Printf("notifying about a disabled feed: %v", err)
		}
	}
}

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

func (f *fetcher) saveToGist(ctx context.Context) error {
	state, err := json.MarshalIndent(f.state, "", "  ")
	if err != nil {
		return err
	}
	feeds, err := json.MarshalIndent(f.feeds, "", "  ")
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

func (f *fetcher) makeTelegramRequest(ctx context.Context, method string, args any) error {
	if _, err := httputil.MakeJSONRequest[any](ctx, httputil.RequestParams{
		Method:     http.MethodPost,
		URL:        tgAPI + "/bot" + f.tgToken + "/" + method,
		Body:       args,
		HTTPClient: f.httpc,
	}); err != nil {
		return err
	}
	return nil
}
