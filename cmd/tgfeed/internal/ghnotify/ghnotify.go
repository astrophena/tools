// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package ghnotify serves GitHub notifications as a JSON Feed.
package ghnotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/web"
)

// Handler serves GitHub notifications for token.
func Handler(token string, logger *slog.Logger, client *http.Client) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &handler{
		token:  token,
		logger: logger,
		client: client,
	}
}

type handler struct {
	token  string
	logger *slog.Logger
	client *http.Client
}

type notification struct {
	ID        string    `json:"id"`
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
	for page := 1; page <= 10; page++ {
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

		// The first page's Date supplies the feed's conditional request timestamp.
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

		allNotifications = append(allNotifications, notifications...)

		if len(notifications) < 100 {
			break
		}
	}

	h.logger.InfoContext(r.Context(), "fetched all notifications",
		slog.Int("total_count", len(allNotifications)),
	)

	var (
		items      []feedItem
		ignoredIDs []string
	)
	for _, n := range allNotifications {
		h.logger.DebugContext(r.Context(), "processing notification",
			slog.String("id", n.ID),
			slog.String("type", n.Subject.Type),
			slog.String("title", n.Subject.Title),
		)

		if n.Subject.Type == "PullRequest" && n.Subject.URL != "" {
			pr, err := request.Make[pullRequest](r.Context(), request.Params{
				URL:        n.Subject.URL,
				Headers:    headers,
				HTTPClient: h.client,
				Scrubber:   strings.NewReplacer(h.token, "REDACTED"),
			})
			switch {
			case err != nil:
				h.logger.ErrorContext(r.Context(), "fetching GitHub PR details failed",
					slog.Any("err", err),
					slog.String("pr_url", n.Subject.URL),
				)
			case pr.User.Login == ignoredAuthor:
				h.logger.InfoContext(r.Context(), "skipping GitHub PR notification from ignored author",
					slog.String("author", pr.User.Login),
					slog.String("pr_url", n.Subject.URL),
				)
				ignoredIDs = append(ignoredIDs, n.ID)
				continue
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
	// Ignored notifications can be marked read immediately.
	if err := MarkAsDone(r.Context(), h.token, h.client, ignoredIDs); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("marking ignored notifications as done failed: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	web.RespondJSON(w, feed)
}

// MarkAsDone marks GitHub notification threads as read after their downstream
// delivery has succeeded.
func MarkAsDone(ctx context.Context, token string, client *http.Client, ids []string) error {
	for _, id := range ids {
		u := fmt.Sprintf("%s/notifications/threads/%s", ghAPI, id)
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		req.Header.Set("User-Agent", "ghnotify (+https://astrophena.name/bleep-bloop)")

		res, err := client.Do(req)
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
	url = strings.ReplaceAll(url, "https://api.github.com/repos/", "https://github.com/")
	// Fix PR links. Replace only the last occurrence of /pulls/ to avoid
	// misidentifying repositories named "pulls".
	if i := strings.LastIndex(url, "/pulls/"); i != -1 {
		url = url[:i] + "/pull/" + url[i+len("/pulls/"):]
	}
	return url
}

const ignoredAuthor = "google-labs-jules[bot]"

type pullRequest struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}
