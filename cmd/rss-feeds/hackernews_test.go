package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildFeed(t *testing.T) {
	s := &hnTop{n: 2}
	now := time.Date(2026, 6, 19, 8, 0, 0, 0, time.UTC)
	hits := []hit{
		{Title: "First & Best", URL: "https://www.example.com/a", Points: 100, NumComments: 50, ObjectID: "1"},
		{Title: "Second", URL: "", Points: 80, NumComments: 30, ObjectID: "2"}, // no URL → HN link
	}

	feed := s.buildFeed(now, hits)

	if got := len(feed.Items); got != 1 {
		t.Fatalf("items = %d, want 1 (single digest entry)", got)
	}
	it := feed.Items[0]
	if want := "Hacker News Daily Top 2 @2026-06-19"; it.Title != want {
		t.Errorf("title = %q, want %q", it.Title, want)
	}
	if !strings.Contains(it.ID, "2026-06-19") {
		t.Errorf("id = %q, want it to embed the date for daily dedup", it.ID)
	}

	for _, want := range []string{
		"<ol>",
		"<b>First &amp; Best</b> example.com</a>",
		"50 comments 100 points",
		"https://news.ycombinator.com/item?id=2",
	} {
		if !strings.Contains(it.Content, want) {
			t.Errorf("content missing %q\ncontent:\n%s", want, it.Content)
		}
	}
}

func TestBuildFeedCapsToN(t *testing.T) {
	s := &hnTop{n: 1}
	now := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
	hits := []hit{{Title: "a", ObjectID: "1"}, {Title: "b", ObjectID: "2"}}

	feed := s.buildFeed(now, hits)
	if n := strings.Count(feed.Items[0].Content, "<li>"); n != 1 {
		t.Errorf("got %d <li>, want 1 (capped to n)", n)
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("hitsPerPage") != "3" {
			t.Errorf("hitsPerPage = %q, want 3", q.Get("hitsPerPage"))
		}
		if !strings.HasPrefix(q.Get("numericFilters"), "created_at_i>") {
			t.Errorf("numericFilters = %q, want a created_at window", q.Get("numericFilters"))
		}
		_, _ = io.WriteString(w, `{"hits":[{"title":"A","url":"https://x.com/a","points":5,"num_comments":2,"objectID":"10"}]}`)
	}))
	defer srv.Close()

	s := &hnTop{
		n:        3,
		endpoint: srv.URL,
		client:   srv.Client(),
		now:      func() time.Time { return time.Date(2026, 6, 19, 8, 0, 0, 0, time.UTC) },
	}

	feed, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(feed.Items[0].Content, `<a href="https://x.com/a">`) {
		t.Errorf("content missing story link:\n%s", feed.Items[0].Content)
	}
}

func TestFetchEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"hits":[]}`)
	}))
	defer srv.Close()

	s := &hnTop{n: 3, endpoint: srv.URL, client: srv.Client(), now: time.Now}
	if _, err := s.Fetch(context.Background()); err == nil {
		t.Error("Fetch: want error on empty hits, got nil")
	}
}
