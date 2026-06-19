package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeSource struct {
	name string
	feed *Feed
	err  error
}

func (f fakeSource) Name() string                         { return f.name }
func (f fakeSource) Fetch(context.Context) (*Feed, error) { return f.feed, f.err }

func TestRun(t *testing.T) {
	ts := time.Date(2026, 6, 19, 8, 0, 0, 0, time.UTC)

	t.Run("merges history and writes", func(t *testing.T) {
		prior := &Feed{Updated: ts, Items: []Item{{ID: "old", Updated: ts}}}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = writeAtom(w, prior, "")
		}))
		defer srv.Close()
		baseURL = srv.URL
		defer func() { baseURL = "https://deadnews.github.io/rss-feeds" }()

		sources = []Source{fakeSource{name: "fake", feed: &Feed{
			Title: "Fake", Updated: ts, Items: []Item{{ID: "new", Updated: ts}},
		}}}

		dir := t.TempDir()
		if err := run(dir); err != nil {
			t.Fatalf("run: %v", err)
		}

		out, err := os.ReadFile(filepath.Join(dir, "fake.xml"))
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		for _, want := range []string{"<id>new</id>", "<id>old</id>"} {
			if !strings.Contains(string(out), want) {
				t.Errorf("output missing %q (history not merged?)\n%s", want, out)
			}
		}
	})

	t.Run("aggregates failures", func(t *testing.T) {
		sources = []Source{fakeSource{name: "bad", err: errors.New("boom")}}
		if err := run(t.TempDir()); err == nil {
			t.Error("run: want error when a source fails")
		}
	})
}
