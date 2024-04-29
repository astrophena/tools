/*
tgfeed fetches RSS feeds and sends new articles via Telegram.

# How it works?

tgfeed runs as a GitHub Actions workflow.

It fetches RSS feeds from URLs provided in the feeds.json file.

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

To perform garbage collection and remove the state of feeds that have been
removed from the list, you can use the -gc flag. This ensures that the program's
state remains up-to-date with the list of feeds. For example:

	$ tgfeed -gc
*/
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.astrophena.name/tools/internal/cli"

	"github.com/mmcdole/gofeed"
)

const (
	defaultErrorTemplate = `❌ Something went wrong:
<pre><code>%v</code></pre>`
	userAgent      = "tgfeed (https://astrophena.name/bleep-bloop)"
	ghAPI          = "https://api.github.com"
	tgAPI          = "https://api.telegram.org"
	errorThreshold = 12 // failing continuously for four days will disable feed and complain loudly
)

func main() {
	var (
		feeds       = flag.Bool("feeds", false, "List subscribed feeds.")
		gc          = flag.Bool("gc", false, "Remove state of feeds that was removed from the list.")
		reenable    = flag.String("reenable", "", "Reenable previously failing and disabled `feed`.")
		subscribe   = flag.String("subscribe", "", "Subscribe to a `feed`.")
		unsubscribe = flag.String("unsubscribe", "", "Unsubscribe from a `feed`.")
	)
	cli.SetDescription("tgfeed fetches RSS feeds and sends new articles via Telegram.")
	cli.SetArgsUsage("[flags]")
	cli.HandleStartup()

	f := &fetcher{
		httpc: &http.Client{
			Timeout: 10 * time.Second,
		},
		gistID:  os.Getenv("GIST_ID"),
		ghToken: os.Getenv("GITHUB_TOKEN"),
		chatID:  os.Getenv("CHAT_ID"),
		tgToken: os.Getenv("TELEGRAM_TOKEN"),
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if *feeds {
		if err := f.listFeeds(ctx, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *gc {
		if err := f.gc(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *reenable != "" {
		if err := f.reenable(ctx, *reenable); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *subscribe != "" {
		if err := f.subscribe(ctx, *subscribe); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *unsubscribe != "" {
		if err := f.unsubscribe(ctx, *unsubscribe); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := f.run(ctx); err != nil {
		f.send(ctx, fmt.Sprintf(f.errorTemplate, err), disableLinkPreview)
		log.Fatal(err)
	}
}

type fetcher struct {
	initOnce sync.Once

	httpc *http.Client
	fp    *gofeed.Parser

	gistID  string
	ghToken string

	feeds   []string
	chatID  string
	tgToken string

	errorTemplate string
	state         map[string]*feedState
	updates       []*gofeed.Item
}

type feedState struct {
	LastModified time.Time `json:"last_modified"`
	LastUpdated  time.Time `json:"last_updated"`
	ETag         string    `json:"etag"`
	Disabled     bool      `json:"disabled"`
	ErrorCount   int       `json:"error_count"`
	LastError    string    `json:"last_error"`

	// Special flags. Not covered by tests or any common sense.

	// Only return updates matching this list of categories.
	FilteredCategories []string `json:"filtered_categories"`
}

func (f *fetcher) doInit() {
	f.fp = gofeed.NewParser()
	f.fp.UserAgent = userAgent
	f.fp.Client = f.httpc

	if f.errorTemplate == "" {
		f.errorTemplate = defaultErrorTemplate
	}
}

func (f *fetcher) run(ctx context.Context) error {
	if err := f.loadFromGist(ctx); err != nil {
		return fmt.Errorf("fetching gist failed: %w", err)
	}
	f.initOnce.Do(f.doInit)

	for _, url := range shuffle(f.feeds) {
		if err := f.fetch(ctx, url); err != nil {
			state := f.state[url]
			state.ErrorCount += 1
			state.LastError = err.Error()
			// Complain loudly and disable feed, if we failed previously enough.
			if state.ErrorCount >= errorThreshold {
				err = fmt.Errorf("fetching feed %q failed after %d previous attempts: %v; feed was disabled, to reenable it run 'tgfeed -reenable %q'", url, state.ErrorCount, err, url)
				state.Disabled = true
				f.send(ctx, fmt.Sprintf(f.errorTemplate, err), disableLinkPreview)
			}
		}
	}

	if len(f.updates) > 0 {
		// Tell the user that we are about to send updates. Don't worry if it fails.
		f.makeTelegramRequest(ctx, "sendChatAction", map[string]any{
			"chat_id": f.chatID,
			"action":  "typing",
		})
	}

	for _, item := range f.updates {
		msg := fmt.Sprintf(
			`<a href="%[1]s">%[2]s</a>`,
			item.Link,
			item.Title,
		)

		inlineKeyboardButtons := []inlineKeyboardButton{}

		// hnrss.org feeds have Hacker News entry URL set as GUID. Also send it
		// because I often read comments on Hacker News entries.
		if strings.HasPrefix(item.GUID, "https://news.ycombinator.com/item?id=") {
			inlineKeyboardButtons = append(inlineKeyboardButtons, inlineKeyboardButton{
				Text: "💬 Comments",
				URL:  item.GUID,
			})
		}

		if err := f.send(ctx, msg, func(args map[string]any) {
			args["link_preview_options"] = linkPreviewOptions{
				URL: item.Link,
			}
			args["reply_markup"] = map[string]any{
				"inline_keyboard": [][]inlineKeyboardButton{inlineKeyboardButtons},
			}
		}); err != nil {
			return fmt.Errorf("sending %q to %q failed: %w", msg, f.chatID, err)
		}
	}

	slices.Sort(f.feeds)
	return f.saveToGist(ctx)
}

func shuffle[S any](s []S) []S {
	s2 := slices.Clone(s)
	rand.Shuffle(len(s2), func(i, j int) {
		s2[i], s2[j] = s2[j], s2[i]
	})
	return s2
}

func (f *fetcher) gc(ctx context.Context) error {
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}

	for url := range f.state {
		var found bool
		for _, existing := range f.feeds {
			if url == existing {
				found = true
				break
			}
		}
		if !found {
			delete(f.state, url)
		}
	}

	return f.saveToGist(ctx)
}

func (f *fetcher) reenable(ctx context.Context, url string) error {
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}

	state, ok := f.state[url]
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
		state, hasState := f.state[url]
		fmt.Fprintf(&sb, "%s", url)
		if !hasState {
			fmt.Fprintf(&sb, " \n")
			continue
		}
		fmt.Fprintf(&sb, " (last updated %s", state.LastUpdated.Format(time.DateTime))
		if state.ErrorCount > 0 {
			failCount := "once"
			if state.ErrorCount > 1 {
				failCount = fmt.Sprintf("%d times", state.ErrorCount)
			}
			fmt.Fprintf(&sb, ", failed %s, last error was %q", failCount, state.LastError)
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
	return f.saveToGist(ctx)
}

type gist struct {
	Files map[string]*gistFile `json:"files"`
}

type gistFile struct {
	Content string `json:"content"`
}

func (f *fetcher) loadFromGist(ctx context.Context) error {
	g, err := f.makeGistRequest(ctx, http.MethodGet, nil)
	if err != nil {
		return err
	}

	errorTemplate, ok := g.Files["error.tmpl"]
	if ok {
		f.errorTemplate = errorTemplate.Content
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

func (f *fetcher) fetch(ctx context.Context, url string) error {
	var firstSaw bool

	if f.state[url] == nil {
		f.state[url] = new(feedState)
		firstSaw = true
	}
	state := f.state[url]

	if state.Disabled {
		return nil
	}

	// If we don't remember this feed, it's probably new. Set it's last update
	// date to current so we don't get a lot of unread articles and trigger
	// Telegram Bot API rate limit.
	if firstSaw {
		state.LastUpdated = time.Now()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", userAgent)
	if state.ETag != "" {
		req.Header.Set("If-None-Match", fmt.Sprintf(`"%s"`, state.ETag))
	}
	if !state.LastModified.IsZero() {
		req.Header.Set("If-Modified-Since", state.LastModified.In(time.UTC).Format(time.RFC1123))
	}

	res, err := f.httpc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// https://http.dev/420
	const statusEnhanceYourCalm = 420

	// Ignore unmodified and rate limited feeds.
	if res.StatusCode == http.StatusNotModified || res.StatusCode == statusEnhanceYourCalm {
		return nil
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("want 200, got %d", res.StatusCode)
	}

	feed, err := f.fp.Parse(res.Body)
	if err != nil {
		return err
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
		f.updates = append(f.updates, item)
	}
	state.LastUpdated = time.Now()
	state.ErrorCount = 0
	state.LastError = ""

	return nil
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
	IsDisabled bool   `json:"is_disabled"`
	URL        string `json:"url"`
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

	data := new(gist)
	data.Files = make(map[string]*gistFile)
	stateFile := &gistFile{Content: string(state)}
	feedsFile := &gistFile{Content: string(feeds)}
	data.Files["feeds.json"] = feedsFile
	data.Files["state.json"] = stateFile

	_, err = f.makeGistRequest(ctx, http.MethodPatch, data)
	return err
}

func (f *fetcher) makeTelegramRequest(ctx context.Context, method string, args any) error {
	if _, err := makeRequest[any](f, ctx, requestParams{
		method: http.MethodPost,
		url:    tgAPI + "/bot" + f.tgToken + "/" + method,
		body:   args,
	}); err != nil {
		return err
	}
	return nil
}

func (f *fetcher) makeGistRequest(ctx context.Context, method string, body any) (*gist, error) {
	return makeRequest[*gist](f, ctx, requestParams{
		method: method,
		url:    ghAPI + "/gists/" + f.gistID,
		headers: map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": "2022-11-28",
			"Authorization":        "Bearer " + f.ghToken,
		},
		body: body,
	})
}

type requestParams struct {
	method  string
	url     string
	headers map[string]string
	body    any
}

func makeRequest[R any](f *fetcher, ctx context.Context, params requestParams) (R, error) {
	var resp R

	var data []byte
	if params.body != nil {
		var err error
		data, err = json.Marshal(params.body)
		if err != nil {
			return resp, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, params.method, params.url, bytes.NewReader(data))
	if err != nil {
		return resp, err
	}

	if params.headers != nil {
		for k, v := range params.headers {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("User-Agent", userAgent)
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := f.httpc.Do(req)
	if err != nil {
		return resp, err
	}
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return resp, err
	}

	if res.StatusCode != http.StatusOK {
		return resp, fmt.Errorf("want 200, got %d: %s", res.StatusCode, b)
	}

	if err := json.Unmarshal(b, &resp); err != nil {
		return resp, err
	}
	return resp, nil
}
