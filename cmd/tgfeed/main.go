/*
tgfeed fetches RSS feeds and sends new articles via Telegram.

# How it works?

tgfeed runs as a GitHub Actions workflow.

It fetches RSS feeds from URLs provided in the feeds.json file. It applies
filters to the fetched articles based on regex rules defined in the filters.json
file. Filters include keep_rule to keep items with titles matching a regex
pattern and ignore_rule to ignore items with titles matching a regex pattern.

New articles are sent to a Telegram chat specified by the CHAT_ID environment
variable. Also tgfeed summarizes articles and videos using YandexGPT through
300.ya.ru API.

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
  - YAGPT_TOKEN: 300.ya.ru access token for summarizing articles using YandexGPT.

# Administration

To update the list of feeds, you can use the -update-feeds flag followed by the
file path containing the updated list of feeds. For example:

	$ tgfeed -update-feeds feeds.json

To update the list of filters, you can use the -update-filters flag followed by
the file path containing the updated list of filters. For example:

	$ tgfeed -update-filters filters.json

To reenable a failing feed that has been disabled due to consecutive failures,
you can use the -reenable flag followed by the URL of the feed. For example:

	$ tgfeed -reenable https://example.com/feed

To view the list of failing feeds, you can use the -failing flag. This will
print the URLs of feeds that have encountered errors during fetching. For
example:

	$ tgfeed -failing

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
	"html"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.astrophena.name/tools/internal/cli"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"github.com/mmcdole/gofeed"
)

const (
	defaultErrorTemplate = `❌ Something went wrong:
<pre><code>%v</code></pre>`
	userAgent      = "tgfeed (https://astrophena.name/bleep-bloop)"
	ghAPI          = "https://api.github.com"
	tgAPI          = "https://api.telegram.org"
	ya300API       = "https://300.ya.ru/api/sharing-url"
	errorThreshold = 12 // failing continuously for four days will disable feed and complain loudly
)

func main() {
	var (
		dryRun        = flag.Bool("dry-run", false, "Fetch feeds, but only print what will be sent and difference between previous and new state.")
		dumpAll       = flag.Bool("dump-all", false, "Dump feeds, filters and state.")
		failing       = flag.Bool("failing", false, "Print the list of failing feeds.")
		gc            = flag.Bool("gc", false, "Remove state of feeds that was removed from the list.")
		updateFeeds   = flag.String("update-feeds", "", "Update feeds list from `file`.")
		updateFilters = flag.String("update-filters", "", "Update feed filters from `file`.")
		reenable      = flag.String("reenable", "", "Reenable previously failing and disabled `feed`.")
	)
	cli.SetDescription("tgfeed fetches RSS feeds and sends new articles via Telegram.")
	cli.SetArgsUsage("[flags]")
	cli.HandleStartup()

	f := &fetcher{
		httpc: &http.Client{
			Timeout: 10 * time.Second,
		},
		dryRun:     *dryRun,
		gistID:     os.Getenv("GIST_ID"),
		ghToken:    os.Getenv("GITHUB_TOKEN"),
		chatID:     os.Getenv("CHAT_ID"),
		tgToken:    os.Getenv("TELEGRAM_TOKEN"),
		ya300Token: os.Getenv("YAGPT_TOKEN"),
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if *dumpAll {
		if err := f.loadFromGist(ctx); err != nil {
			log.Fatal(err)
		}
		log.Println("=== Feeds ===")
		spew.Fdump(os.Stderr, f.feeds)
		log.Println("=== Filters ===")
		spew.Fdump(os.Stderr, f.filters)
		log.Println("=== State ===")
		spew.Fdump(os.Stderr, f.state)
		return
	}

	if *failing {
		if err := f.loadFromGist(ctx); err != nil {
			log.Fatal(err)
		}
		for url, state := range f.state {
			if state.ErrorCount == 0 {
				continue
			}
			failText := "once"
			if state.ErrorCount > 1 {
				failText = fmt.Sprintf("%d times", state.ErrorCount)
			}
			log.Printf("%s (failed %s, last error: %q)", url, failText, state.LastError)
		}
		return
	}

	if *gc {
		if err := f.gc(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *updateFeeds != "" {
		if err := f.updateFeedsFromFile(ctx, *updateFeeds); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *updateFilters != "" {
		if err := f.updateFiltersFromFile(ctx, *updateFilters); err != nil {
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

	if err := f.run(ctx); err != nil {
		f.send(ctx, fmt.Sprintf(f.errorTemplate, err))
		log.Fatal(err)
	}
}

type fetcher struct {
	initOnce sync.Once

	httpc *http.Client
	fp    *gofeed.Parser

	dryRun bool
	log    *log.Logger

	gistID  string
	ghToken string

	feeds      []string
	filters    map[string]*feedFilter
	chatID     string
	tgToken    string
	ya300Token string

	errorTemplate    string
	state, prevState map[string]*feedState
	updates          []*gofeed.Item
}

type feedFilter struct {
	KeepRule   string `json:"keep_rule"`   // keep only the items with a title that matches the regex
	IgnoreRule string `json:"ignore_rule"` // ignore items with a title that match the regex; ignored when KeepRule is set

	keepRe, ignoreRe *regexp.Regexp // compiled rules
}

type feedState struct {
	LastModified time.Time `json:"last_modified"`
	LastUpdated  time.Time `json:"last_updated"`
	ETag         string    `json:"etag"`
	Disabled     bool      `json:"disabled"`
	ErrorCount   int       `json:"error_count"`
	LastError    string    `json:"last_error"`
}

func (f *fetcher) doInit() {
	f.fp = gofeed.NewParser()
	f.fp.UserAgent = userAgent
	f.fp.Client = f.httpc

	if f.errorTemplate == "" {
		f.errorTemplate = defaultErrorTemplate
	}

	if f.log == nil {
		f.log = log.New(os.Stderr, "", 0)
	}
}

func (f *fetcher) run(ctx context.Context) error {
	if err := f.loadFromGist(ctx); err != nil {
		return fmt.Errorf("fetching gist failed: %w", err)
	}
	f.initOnce.Do(f.doInit)

	for _, url := range f.feeds {
		if err := f.fetch(ctx, url); err != nil {
			state := f.state[url]
			// Complain loudly and disable feed, if we failed previously enough.
			if state.ErrorCount >= errorThreshold {
				state.LastError = err.Error()
				state.ErrorCount += 1
				err = fmt.Errorf("fetching feed %q failed after %d previous attempts: %v; feed was disabled, to reenable it run 'tgfeed -reenable %q'", url, state.ErrorCount, err, url)
				state.Disabled = true
				f.send(ctx, fmt.Sprintf(f.errorTemplate, err))
				continue
			}
			// Otherwise, carry on.
			state.ErrorCount += 1
			state.LastError = err.Error()
		}
	}

	for _, item := range f.updates {
		msg := fmt.Sprintf(
			`<a href="%[1]s">%[2]s</a>`,
			item.Link,
			html.EscapeString(item.Title),
		)

		// hnrss.org feeds have Hacker News entry URL set as GUID. Also send it
		// because I often read comments on Hacker News entries.
		if strings.HasPrefix(item.GUID, "https://news.ycombinator.com/item?id=") {
			msg += fmt.Sprintf("\n\n💬 <a href=\"%s\">Comments</a>", item.GUID)
		}

		if f.dryRun {
			f.log.Println(msg)
			continue
		}

		if f.ya300Token != "" {
			if summaryURL, err := f.summarize(ctx, item.Link); err != nil {
				f.log.Printf("summarizing article %q using 300.ya.ru failed: %v", item.Link, err)
			} else {
				msg += fmt.Sprintf("\n\n ℹ️ <a href=\"%s\">Summary</a>", summaryURL)
			}
		}

		if err := f.send(ctx, msg); err != nil {
			return fmt.Errorf("sending %q to %q failed: %w", msg, f.chatID, err)
		}
	}

	return f.saveToGist(ctx)
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

func (f *fetcher) updateFiltersFromFile(ctx context.Context, file string) error {
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}

	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &f.filters); err != nil {
		return err
	}

	return f.saveToGist(ctx)
}

func (f *fetcher) updateFeedsFromFile(ctx context.Context, file string) error {
	if err := f.loadFromGist(ctx); err != nil {
		return err
	}

	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &f.feeds); err != nil {
		return err
	}
	slices.Sort(f.feeds)

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
		if err := json.Unmarshal([]byte(state.Content), &f.state); err != nil {
			return err
		}
	}
	f.prevState = maps.Clone(f.state)

	f.filters = make(map[string]*feedFilter)
	filters, ok := g.Files["filters.json"]
	if ok {
		return json.Unmarshal([]byte(filters.Content), &f.filters)
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
	req.Header.Set("If-Modified-Since", state.LastModified.In(time.UTC).Format(time.RFC1123))

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

	// Try to parse a feed filter, if it does exist.
	filter, ok := f.filters[url]
	if ok {
		if filter.KeepRule != "" {
			keepRe, err := regexp.Compile(filter.KeepRule)
			if err == nil {
				filter.keepRe = keepRe
			} else {
				f.log.Printf("%s: invalid keep rule %q: %v", url, filter.KeepRule, err)
			}
		}

		if filter.keepRe == nil && filter.IgnoreRule != "" {
			ignoreRe, err := regexp.Compile(filter.IgnoreRule)
			if err == nil {
				filter.ignoreRe = ignoreRe
			} else {
				f.log.Printf("%s: invalid ignore rule %q: %v", url, filter.IgnoreRule, err)
			}
		}
	}

	for _, item := range feed.Items {
		if item.PublishedParsed.Before(state.LastUpdated) {
			continue
		}
		if filter != nil && filter.keepRe != nil && !filter.keepRe.MatchString(item.Title) {
			continue
		}
		if filter != nil && filter.ignoreRe != nil && filter.ignoreRe.MatchString(item.Title) {
			continue
		}
		f.updates = append(f.updates, item)
	}
	state.LastUpdated = time.Now()
	state.ErrorCount = 0
	state.LastError = ""

	return nil
}

func (f *fetcher) send(ctx context.Context, message string) error {
	return f.makeTelegramRequest(ctx, "sendMessage", map[string]string{
		"chat_id":    f.chatID,
		"parse_mode": "HTML",
		"text":       message,
	})
}

func (f *fetcher) summarize(ctx context.Context, url string) (sharingURL string, err error) {
	type responseSchema struct {
		Status     string `json:"status"`
		SharingURL string `json:"sharing_url"`
	}

	resp, err := makeRequest[*responseSchema](f, ctx, requestParams{
		method: http.MethodPost,
		url:    ya300API,
		body:   strings.NewReader(fmt.Sprintf(`{"article_url":"%s"}`, url)),
		headers: map[string]string{
			"Authorization": "OAuth " + f.ya300Token,
			"Content-Type":  "application/json",
		},
	})
	if err != nil {
		return "", err
	}

	if resp.Status != "success" {
		return "", fmt.Errorf("want status success, got %s", resp.Status)
	}
	return resp.SharingURL, nil
}

var inTest bool // set true when testing

func (f *fetcher) saveToGist(ctx context.Context) error {
	// Don't save to gist in dry run mode. Print a diff instead between previous
	// state and new state. Except inside tests.
	if f.dryRun {
		if !inTest {
			if diff := cmp.Diff(f.prevState, f.state); diff != "" {
				f.log.Printf("state changed:\n%s", diff)
			}
		}
		return nil
	}

	state, err := json.MarshalIndent(f.state, "", "  ")
	if err != nil {
		return err
	}
	feeds, err := json.MarshalIndent(f.feeds, "", "  ")
	if err != nil {
		return err
	}
	filters, err := json.MarshalIndent(f.filters, "", "  ")
	if err != nil {
		return err
	}

	data := new(gist)
	data.Files = make(map[string]*gistFile)
	stateFile := &gistFile{Content: string(state)}
	feedsFile := &gistFile{Content: string(feeds)}
	filtersFile := &gistFile{Content: string(filters)}
	data.Files["feeds.json"] = feedsFile
	data.Files["state.json"] = stateFile
	data.Files["filters.json"] = filtersFile

	_, err = f.makeGistRequest(ctx, http.MethodPatch, data)
	return err
}

func (f *fetcher) makeTelegramRequest(ctx context.Context, method string, args map[string]string) error {
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
