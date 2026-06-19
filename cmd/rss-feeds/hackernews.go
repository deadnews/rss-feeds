package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const algoliaSearchURL = "https://hn.algolia.com/api/v1/search"

// hnTop emits a daily digest of the top-n HN stories by points over the last ~25h.
type hnTop struct {
	n        int
	endpoint string
	client   *http.Client
	now      func() time.Time // injectable for tests
}

// HackerNewsTop returns a Source for the daily top-n HN stories.
func HackerNewsTop(n int) Source {
	return &hnTop{
		n:        n,
		endpoint: algoliaSearchURL,
		client:   &http.Client{Timeout: 30 * time.Second},
		now:      time.Now,
	}
}

func (s *hnTop) Name() string { return fmt.Sprintf("hackernews-top%d", s.n) }

type hit struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Points      int    `json:"points"`
	NumComments int    `json:"num_comments"`
	ObjectID    string `json:"objectID"`
}

func (h hit) commentsURL() string {
	return "https://news.ycombinator.com/item?id=" + h.ObjectID
}

// Fetch queries Algolia for the top stories of the past day and builds the feed.
func (s *hnTop) Fetch(ctx context.Context) (*Feed, error) {
	now := s.now().UTC()
	end := now.Unix()
	start := end - 25*60*60 // 1h overlap to catch late-posted stories

	q := url.Values{
		"tags":           {"story"},
		"hitsPerPage":    {strconv.Itoa(s.n)},
		"numericFilters": {fmt.Sprintf("created_at_i>%d,created_at_i<%d", start, end)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("algolia status %d", resp.StatusCode)
	}

	var body struct {
		Hits []hit `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(body.Hits) == 0 {
		return nil, errors.New("no stories returned")
	}

	return s.buildFeed(now, body.Hits), nil
}

// buildFeed renders hits into a one-entry daily digest feed.
func (s *hnTop) buildFeed(now time.Time, hits []hit) *Feed {
	if len(hits) > s.n {
		hits = hits[:s.n]
	}
	date := now.Format("2006-01-02")
	return &Feed{
		Title:   fmt.Sprintf("Hacker News Daily Top %d", s.n),
		Link:    "https://news.ycombinator.com/",
		Updated: now,
		Items: []Item{{
			// Stable per day so a poller delivers each digest once.
			ID:      fmt.Sprintf("tag:news.ycombinator.com,%s:top%d", date, s.n),
			Title:   fmt.Sprintf("Hacker News Daily Top %d @%s", s.n, date),
			Link:    "https://news.ycombinator.com/",
			Updated: now,
			Content: renderList(hits),
		}},
	}
}

// renderList builds an <ol> of stories. Feed readers show a numbered list;
// rss2tg's sanitizer turns <li> into "1.", "2." … with newlines.
func renderList(hits []hit) string {
	var b strings.Builder
	b.WriteString("<ol>\n")
	for _, h := range hits {
		link := h.URL
		if link == "" {
			link = h.commentsURL()
		}
		b.WriteString("<li>")
		fmt.Fprintf(&b, `<a href="%s"><b>%s</b>`, html.EscapeString(link), html.EscapeString(h.Title))
		if host := hostname(link); host != "" {
			fmt.Fprintf(&b, " %s", html.EscapeString(host))
		}
		b.WriteString("</a>")
		fmt.Fprintf(&b, ` - <a href="%s">%d comments %d points</a>`, html.EscapeString(h.commentsURL()), h.NumComments, h.Points)
		b.WriteString("</li>\n")
	}
	b.WriteString("</ol>")
	return b.String()
}

// hostname returns the host without a leading "www.".
func hostname(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
