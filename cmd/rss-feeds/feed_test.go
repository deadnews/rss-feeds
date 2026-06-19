package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWriteAtom(t *testing.T) {
	ts := time.Date(2026, 6, 19, 8, 0, 0, 0, time.UTC)
	f := &Feed{
		Title:   "Title",
		Link:    "https://example.com/",
		Updated: ts,
		Items: []Item{{
			ID:      "id-1",
			Title:   "Entry",
			Link:    "https://example.com/a",
			Content: "<ol><li>x</li></ol>",
			Updated: ts,
		}},
	}

	var buf bytes.Buffer
	if err := writeAtom(&buf, f, "https://example.com/feed.xml"); err != nil {
		t.Fatalf("writeAtom: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		`<feed xmlns="http://www.w3.org/2005/Atom">`,
		`rel="self"`,
		"&lt;ol&gt;", // HTML content must be escaped inside <content>
		"2026-06-19T08:00:00Z",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}

	// Must be well-formed XML.
	dec := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("invalid XML: %v", err)
		}
	}
}

func TestMergeHistory(t *testing.T) {
	current := []Item{{ID: "c"}}
	prior := []Item{{ID: "c"}, {ID: "b"}, {ID: "a"}} // "c" is a same-day re-run

	got := mergeHistory(current, prior, 2)
	want := []string{"c", "b"} // current first, dup dropped, capped to 2
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("entry[%d] = %q, want %q", i, got[i].ID, id)
		}
	}
}

func TestFetchHistory(t *testing.T) {
	feed := &Feed{
		Title:   "T",
		Updated: time.Date(2026, 6, 18, 8, 0, 0, 0, time.UTC),
		Items:   []Item{{ID: "x", Title: "X", Link: "https://e.com/x", Content: "<b>x</b>", Updated: time.Date(2026, 6, 18, 8, 0, 0, 0, time.UTC)}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = writeAtom(w, feed, "https://e.com/feed.xml")
	}))
	defer srv.Close()

	items := fetchHistory(context.Background(), srv.URL)
	if len(items) != 1 || items[0].ID != "x" || items[0].Content != "<b>x</b>" {
		t.Errorf("round-trip mismatch: %+v", items)
	}
}

func TestFetchHistoryMissing(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	if items := fetchHistory(context.Background(), srv.URL); items != nil {
		t.Errorf("got %v, want nil for missing feed", items)
	}
}

func TestFetchHistoryLogsTransientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "not xml")
	}))
	defer srv.Close()

	var logs strings.Builder
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	if items := fetchHistory(context.Background(), srv.URL); items != nil {
		t.Errorf("got %v, want nil on decode error", items)
	}
	if !strings.Contains(logs.String(), "history") {
		t.Errorf("expected a logged warning, got %q", logs.String())
	}
}
