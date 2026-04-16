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
	"log"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	"go.astrophena.name/tools/cmd/tgfeed/internal/admin"
	"go.astrophena.name/tools/cmd/tgfeed/internal/diff"
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"
	"go.astrophena.name/tools/cmd/tgfeed/internal/telegram"
	"go.astrophena.name/tools/internal/filelock"

	"github.com/mmcdole/gofeed"
)

//go:embed defaults/error.tmpl
var defaultErrorTemplate string

// Some types of errors that can happen during tgfeed execution.
var (
	errAlreadyRunning = errors.New("already running")
	errNoFeed         = errors.New("no such feed")
	errNoEditor       = errors.New("environment variable EDITOR is not defined")
)

// Entry point and program state.

func main() { cli.Main(new(fetcher)) }

type fetcher struct {
	running atomic.Bool
	init    sync.Once

	// configuration
	adminAddr     string
	chatID        string
	dry           bool
	errorThreadID int64
	ghToken       string
	remoteURL     string
	stateDir      string
	tgToken       string

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
	state         *state.FeedSet

	stats      syncx.Protected[*stats.Run]
	statsStore *stats.Store
	sender     sender.Sender
	store      state.Store

	runLock filelock.Lock
}

// Bootstrap and commands.

func (f *fetcher) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&f.dry, "dry", false, "Enable dry-run mode: log actions, but don't send updates or save state.")
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

	f.init.Do(func() {
		f.doInit(ctx)
	})

	if f.dry {
		f.slogLevel.Set(slog.LevelDebug)
	}

	if len(env.Args) == 0 {
		return fmt.Errorf("%w: command is required, see -help for usage", cli.ErrInvalidArgs)
	}

	switch env.Args[0] {
	case "admin":
		reader := stats.OpenReader(f.stateDir)
		if err := reader.Bootstrap(ctx); err != nil {
			return fmt.Errorf("bootstrapping stats database failed: %w", err)
		}
		return admin.Run(ctx, admin.Config{
			Addr:       f.adminAddr,
			StateDir:   f.stateDir,
			Store:      f.store,
			StatsStore: reader,
			ValidateConfig: func(ctx context.Context, content string) error {
				_, err := f.parseConfig(ctx, content)
				return err
			},
			IsRunLocked: f.isRunLocked,
		})
	case "feeds":
		return f.listFeeds(ctx, env.Stdout)
	case "edit":
		return f.edit(ctx)
	case "run":
		const maxRunTime = 10 * time.Minute
		rctx, cancel := context.WithTimeout(ctx, maxRunTime)
		defer cancel()
		if err := f.run(rctx); err != nil {
			return f.errNotify(ctx, err)
		}
		return nil
	case "reenable":
		if len(env.Args) != 2 {
			return fmt.Errorf("%w: reenable command expects a feed URL", cli.ErrInvalidArgs)
		}
		return f.reenable(ctx, env.Args[1])
	default:
		return fmt.Errorf("%w: no such command %q", cli.ErrInvalidArgs, env.Args[0])
	}
}

// Run orchestration.

func (f *fetcher) run(ctx context.Context) error {
	if f.running.Load() {
		return errAlreadyRunning
	}
	f.running.Store(true)
	defer f.running.Store(false)

	if err := f.acquireRunLock(); err != nil {
		return err
	}
	defer f.releaseRunLock()

	f.stats = syncx.Protect(&stats.Run{
		StartTime: time.Now(),
	})
	f.statsStore = stats.OpenWriter(f.stateDir)
	if err := f.statsStore.Bootstrap(ctx); err != nil {
		return fmt.Errorf("bootstrapping stats database failed: %w", err)
	}
	if migrated, err := f.statsStore.MigrateJSONDir(ctx, filepath.Join(f.stateDir, "stats")); err != nil {
		return fmt.Errorf("migrating legacy stats failed: %w", err)
	} else if migrated > 0 {
		f.slog.Info("migrated legacy stats into SQLite", "count", migrated)
	}

	if err := f.loadState(ctx); err != nil {
		return fmt.Errorf("loading state failed: %w", err)
	}

	// Buffered updates prevent fetch workers from stalling on Telegram delivery.
	updates := make(chan *update, 100)

	var baseWg sync.WaitGroup
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

	for _, feed := range f.feeds {
		if fdState, ok := f.state.Get(feed.url); ok && fdState.DisabledNotifyPending {
			err := fmt.Errorf("fetching feed %q failed after %d previous attempts: feed was disabled, to reenable it run 'tgfeed reenable %q'", feed.url, fdState.ErrorCount, feed.url)
			if nErr := f.errNotify(ctx, err); nErr != nil {
				f.slog.Warn("failed to send pending error notification", "feed", feed.url, "error", nErr)
			} else {
				if updateErr := f.withFeedState(ctx, feed.url, func(state *state.Feed, _ bool) bool {
					state.DisabledNotifyPending = false
					return true
				}); updateErr != nil {
					f.slog.Warn("failed to persist feed failure state (DisabledNotifyPending)", "feed", feed.url, "error", updateErr)
				}
			}
		}
	}

	fetchWg := syncx.NewLimitedWaitGroup(fetchConcurrencyLimit)
	for _, feed := range shuffle(f.feeds) {
		fetchWg.Go(func() {
			defer fetchedFeeds.Add(1)
			f.runFeedFetch(ctx, feed, updates)
		})
	}

	fetchWg.Wait()
	close(updates)
	baseWg.Wait()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	f.stats.WriteAccess(func(s *stats.Run) {
		s.Duration = time.Since(s.StartTime)
		s.TotalFeeds = len(f.feeds)
		if s.SuccessFeeds > 0 {
			s.AvgFetchTime = s.TotalFetchTime / time.Duration(s.SuccessFeeds)
		}
		s.FetchLatencyMS = stats.QuantilesMS(s.FetchLatencySamples)
		s.SendLatencyMS = stats.QuantilesMS(s.SendLatencySamples)

		stateSnapshot := f.state.Snapshot()
		for _, feedState := range stateSnapshot {
			s.SeenItemsEntriesTotal += len(feedState.SeenItems)
		}
		if stateBytes, err := state.MarshalStateMap(stateSnapshot); err == nil {
			s.StateBytesWritten = len(stateBytes)
		}

		s.TopSlowestFeeds = stats.TopFeedStats(s.FeedStatsByURL, func(a *stats.FeedStats, b *stats.FeedStats) int {
			if a.FetchDuration == b.FetchDuration {
				return strings.Compare(a.URL, b.URL)
			}
			if a.FetchDuration > b.FetchDuration {
				return -1
			}
			return 1
		})
		s.TopErrorFeeds = stats.TopFeedStats(s.FeedStatsByURL, func(a *stats.FeedStats, b *stats.FeedStats) int {
			if a.Failures == b.Failures {
				return strings.Compare(a.URL, b.URL)
			}
			if a.Failures > b.Failures {
				return -1
			}
			return 1
		})
		s.TopNewItemFeeds = stats.TopFeedStats(s.FeedStatsByURL, func(a *stats.FeedStats, b *stats.FeedStats) int {
			if a.ItemsEnqueued == b.ItemsEnqueued {
				return strings.Compare(a.URL, b.URL)
			}
			if a.ItemsEnqueued > b.ItemsEnqueued {
				return -1
			}
			return 1
		})
		s.MemoryUsage = m.Alloc
	})

	if err := f.cleanState(ctx); err != nil {
		return err
	}

	f.slog.Debug("fetch finished", "fetched_count", fetchedFeeds.Load(), "all_count", len(f.feeds))

	if f.dry {
		return nil
	}

	f.stats.ReadAccess(func(s *stats.Run) {
		if err := f.statsStore.SaveRun(ctx, s); err != nil {
			f.slog.Warn("failed to upload stats", "error", err)
		}
	})

	return nil
}

func (f *fetcher) runFeedFetch(ctx context.Context, fd *feed, updates chan *update) {
	var retries int
	var feedRetried bool

	for {
		retry, retryIn := f.fetch(ctx, fd, updates)
		if !retry {
			break
		}
		if !f.retryFeedFetch(ctx, fd, retries, retryIn) {
			break
		}
		feedRetried = true
		retries += 1
	}

	if feedRetried {
		f.stats.WriteAccess(func(s *stats.Run) {
			s.FeedsRetriedCount += 1
		})
	}
}

func (f *fetcher) retryFeedFetch(ctx context.Context, fd *feed, retries int, retryIn time.Duration) bool {
	if retries >= retryLimit {
		f.handleFetchFailure(ctx, fd.url, fmt.Errorf("retry limit exceeded after %d retries", retries))
		return false
	}
	if retryIn > maxRetryTime {
		f.slog.Warn("feed retry time is too long, not retrying at all",
			"feed", fd.url,
			"retry_in", retryIn.String(),
			"max_retry_time", maxRetryTime.String(),
		)
		f.handleFetchFailure(ctx, fd.url, fmt.Errorf("retry wait %s exceeds max retry time %s", retryIn, maxRetryTime))
		return false
	}

	f.stats.WriteAccess(func(s *stats.Run) {
		s.FetchRetriesTotal += 1
		s.BackoffSleepTotal += retryIn
		s.FeedStats(fd.url).Retries += 1
		s.SpecialRateLimitRetries += 1
	})
	f.slog.Warn("retrying feed",
		"feed", fd.url,
		"retry_in", retryIn.String(),
		"retries", retries+1,
		"retry_limit", retryLimit,
	)
	return sleep(ctx, retryIn)
}

func (f *fetcher) cleanState(ctx context.Context) error {
	keep := make(map[string]struct{}, len(f.feeds))
	for _, fd := range f.feeds {
		keep[fd.url] = struct{}{}
	}
	return f.state.PruneMissing(ctx, keep)
}

// User-facing commands.

func (f *fetcher) listFeeds(ctx context.Context, w io.Writer) error {
	if err := f.loadState(ctx); err != nil {
		return err
	}

	var sb strings.Builder
	for _, feed := range f.feeds {
		state, hasState := f.state.Get(feed.url)
		fmt.Fprintf(&sb, "%s", feed.url)
		if !hasState {
			fmt.Fprintf(&sb, " \n")
			continue
		}

		fmt.Fprintf(&sb, " (")
		if feed.title != "" {
			fmt.Fprintf(&sb, "%q, ", feed.title)
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

	editorPath, err := exec.LookPath(editor)
	if err != nil {
		return fmt.Errorf("invalid EDITOR %q: %w", editor, err)
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
		cmd := exec.Command(editorPath, tmpfile.Name())
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

	return f.store.SaveConfig(ctx, f.config)
}

func (f *fetcher) reenable(ctx context.Context, url string) error {
	if err := f.loadState(ctx); err != nil {
		return err
	}

	_, ok := f.state.Get(url)
	if !ok {
		return fmt.Errorf("%q: %w", url, errNoFeed)
	}

	return f.withFeedState(ctx, url, func(fdState *state.Feed, _ bool) bool {
		fdState.Reenable()
		return true
	})
}

// Low-level helpers.

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

func parseInt(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return i
	}
	return 0
}

// sleep waits for the duration unless the context is canceled first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func shuffle[S any](s []S) []S {
	s2 := slices.Clone(s)
	rand.Shuffle(len(s2), func(i, j int) {
		s2[i], s2[j] = s2[j], s2[i]
	})
	return s2
}

func (f *fetcher) doInit(ctx context.Context) {
	env := cli.GetEnv(ctx)
	f.logf = log.New(env.Stderr, "", 0).Printf

	if f.httpc == nil {
		f.httpc = request.DefaultClient
	}
	f.fp = gofeed.NewParser()

	if f.tgToken != "" {
		f.scrubber = strings.NewReplacer(f.tgToken, "[EXPUNGED]")
	}

	l := logger.Get(ctx)
	f.slogLevel = l.Level
	f.slog = l.Logger

	if f.sender == nil {
		f.sender = telegram.New(telegram.Config{
			ChatID:     f.chatID,
			Token:      f.tgToken,
			HTTPClient: f.httpc,
			Scrubber:   f.scrubber,
			Logger:     f.slog,
		})
	}

	if f.store == nil {
		f.store = state.NewStore(state.Options{
			StateDir:             f.stateDir,
			RemoteURL:            f.remoteURL,
			HTTPClient:           f.httpc,
			DefaultErrorTemplate: defaultErrorTemplate,
		})
	}
}
