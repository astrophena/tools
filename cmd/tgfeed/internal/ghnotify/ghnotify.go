// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package ghnotify implements an HTTP handler that serves a JSON Feed of GitHub
// notifications.
package ghnotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/web"
)

// Handler returns an HTTP handler that serves a JSON Feed of GitHub
// notifications.
func Handler(token string, logger *slog.Logger, client *http.Client) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &handler{token: token, logger: logger, client: client}
}

type handler struct {
	token  string
	logger *slog.Logger
	client *http.Client
}

type notification struct {
	ID        string    `json:"id"`
	Unread    bool      `json:"unread"`
	Reason    string    `json:"reason"`
	UpdatedAt time.Time `json:"updated_at"`
	Subject   struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Type  string `json:"type"`
	} `json:"subject"`
	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
}

type jsonFeed struct {
	Version     string     `json:"version"`
	Title       string     `json:"title"`
	HomePageURL string     `json:"home_page_url"`
	FeedURL     string     `json:"feed_url"`
	Items       []feedItem `json:"items"`
}

type feedItem struct {
	ID            string    `json:"id"`
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	ContentText   string    `json:"content_text"`
	DatePublished time.Time `json:"date_published"`
	ExternalURL   string    `json:"external_url"`
}

const ghAPI = "https://api.github.com"

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	headers := map[string]string{
		"Authorization": "Bearer " + h.token,
		"Accept":        "application/vnd.github.v3+json",
		"User-Agent":    "ghnotify (+https://astrophena.name/bleep-bloop)",
	}

	var allNotifications []notification
	page := 1

	for {
		u := fmt.Sprintf("%s/notifications?page=%d&per_page=100", ghAPI, page)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
		if err != nil {
			web.RespondJSONError(w, r, err)
			return
		}

		for key, value := range headers {
			req.Header.Set(key, value)
		}

		if lastModified := r.Header.Get("If-Modified-Since"); lastModified != "" {
			req.Header.Set("If-Modified-Since", lastModified)
		}

		res, err := h.client.Do(req)
		if err != nil {
			web.RespondJSONError(w, r, err)
			return
		}

		if res.StatusCode == http.StatusNotModified {
			res.Body.Close()
			// If the first page is not modified, then nothing is modified.
			if page == 1 {
				h.logger.DebugContext(r.Context(), "notifications not modified")
				w.WriteHeader(http.StatusNotModified)
				return
			}
			break
		}

		b, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			web.RespondJSONError(w, r, err)
			return
		}

		if res.StatusCode != http.StatusOK {
			web.RespondJSONError(w, r, fmt.Errorf("want 200, got %d: %s", res.StatusCode, b))
			return
		}

		// GitHub docs mention that Last-Modified header is set, but for some reason
		// it actually doesn't.
		if page == 1 {
			w.Header().Set("Last-Modified", res.Header.Get("Date"))
		}

		var notifications []notification
		if err := json.Unmarshal(b, &notifications); err != nil {
			web.RespondJSONError(w, r, err)
			return
		}

		h.logger.DebugContext(r.Context(), "fetched notifications page",
			slog.Int("page", page),
			slog.Int("count", len(notifications)),
		)

		if len(notifications) == 0 {
			break
		}

		allNotifications = append(allNotifications, notifications...)

		if len(notifications) < 100 {
			break
		}
		page++

		// Safety brake to prevent infinite loops.
		if page > 10 {
			break
		}
	}

	h.logger.InfoContext(r.Context(), "fetched all notifications",
		slog.Int("total_count", len(allNotifications)),
	)

	var items []feedItem
	for _, n := range allNotifications {
		h.logger.DebugContext(r.Context(), "processing notification",
			slog.String("id", n.ID),
			slog.String("type", n.Subject.Type),
			slog.String("title", n.Subject.Title),
		)

		if n.Subject.Type == "PullRequest" {
			prURL := n.Subject.URL
			if prURL != "" {
				pr, err := request.Make[pullRequest](r.Context(), request.Params{
					Method:     http.MethodGet,
					URL:        prURL,
					Headers:    headers,
					HTTPClient: h.client,
					Scrubber:   strings.NewReplacer(h.token, "REDACTED"),
				})
				if err == nil {
					if slices.Contains(ignoredAuthors, pr.User.Login) {
						h.logger.InfoContext(r.Context(), "skipping GitHub PR notification from ignored author",
							slog.String("author", pr.User.Login),
							slog.String("pr_url", prURL),
						)
						continue
					}
				} else {
					h.logger.ErrorContext(r.Context(), "fetching GitHub PR details failed",
						slog.Any("err", err),
						slog.String("pr_url", prURL),
					)
				}
			}
		}

		url := rewriteURL(n.Subject.URL)
		if url == "" {
			url = n.Repository.HTMLURL
		}
		if n.Reason == "ci_activity" {
			url = n.Repository.HTMLURL + "/actions"
		}
		item := feedItem{
			ID:            n.ID,
			URL:           url,
			Title:         fmt.Sprintf("%s: %s", n.Repository.FullName, n.Subject.Title),
			ContentText:   fmt.Sprintf("%s (%s)", n.Subject.Title, n.Reason),
			DatePublished: n.UpdatedAt,
			ExternalURL:   n.Repository.HTMLURL,
		}
		items = append(items, item)
	}

	feed := jsonFeed{
		Version:     "https://jsonfeed.org/version/1.1",
		Title:       "GitHub Notifications",
		HomePageURL: "https://github.com",
		Items:       items,
	}

	if err := h.markAsDone(r.Context(), allNotifications); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("marking notifications as done failed: %v", err))
		return
	}

	h.logger.InfoContext(r.Context(), "marked notifications as done",
		slog.Int("count", len(allNotifications)),
	)

	w.Header().Set("Content-Type", "application/json")
	web.RespondJSON(w, feed)
}

func (h *handler) markAsDone(ctx context.Context, notifications []notification) error {
	for _, n := range notifications {
		u := fmt.Sprintf("%s/notifications/threads/%s", ghAPI, n.ID)
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", "Bearer "+h.token)
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		req.Header.Set("User-Agent", "ghnotify (+https://astrophena.name/bleep-bloop)")

		res, err := h.client.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()

		if res.StatusCode != http.StatusNoContent {
			return fmt.Errorf("want 204, got %d", res.StatusCode)
		}
	}
	return nil
}

func rewriteURL(url string) string {
	if url == "" {
		return ""
	}
	url = strings.ReplaceAll(url, "https://api.github.com/repos/", "https://github.com/")
	url = strings.ReplaceAll(url, "pulls", "pull") // fix PR links
	return url
}

// ignoredAuthors is a list of GitHub usernames whose PRs should be ignored.
var ignoredAuthors = []string{
	// I create PRs using Jules manually, so I don't want to be notified about them.
	"google-labs-jules[bot]",
}

type pullRequest struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}
