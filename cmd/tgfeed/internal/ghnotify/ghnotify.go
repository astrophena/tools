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
	"net/http"
	"strings"
	"time"

	"go.astrophena.name/base/web"
)

// Handler returns an HTTP handler that serves a JSON Feed of GitHub
// notifications.
func Handler(token string, client *http.Client) http.Handler {
	return &handler{token: token, client: client}
}

type handler struct {
	token  string
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
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, ghAPI+"/notifications", nil)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "ghnotify (+https://astrophena.name/bleep-bloop)")

	if lastModified := r.Header.Get("If-Modified-Since"); lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	res, err := h.client.Do(req)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	b, err := io.ReadAll(res.Body)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	if res.StatusCode != http.StatusOK {
		web.RespondJSONError(w, r, fmt.Errorf("want 200, got %d: %s", res.StatusCode, b))
		return
	}

	var notifications []notification
	if err := json.Unmarshal(b, &notifications); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	var items []feedItem
	for _, n := range notifications {
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

	if err := h.markAsRead(r.Context()); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("marking notifications as read failed: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// GitHub docs mention that Last-Modified header is set, but for some reason
	// it actually doesn't.
	w.Header().Set("Last-Modified", res.Header.Get("Date"))
	web.RespondJSON(w, feed)
}

func (h *handler) markAsRead(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, ghAPI+"/notifications", nil)
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
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode == http.StatusResetContent || res.StatusCode == http.StatusAccepted {
		return nil
	}
	return fmt.Errorf("want 205 or 202, got %d: %s", res.StatusCode, b)
}

func rewriteURL(url string) string {
	if url == "" {
		return ""
	}
	url = strings.ReplaceAll(url, "https://api.github.com/repos/", "https://github.com/")
	url = strings.ReplaceAll(url, "pulls", "pull") // fix PR links
	return url
}
