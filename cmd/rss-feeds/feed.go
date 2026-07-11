package main

import (
	"cmp"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Source produces one Atom feed from some upstream.
type Source interface {
	Name() string
	Fetch(ctx context.Context) (*Feed, error)
}

// Feed is a source-agnostic feed model rendered to Atom by writeAtom.
type Feed struct {
	Title   string
	Link    string
	Updated time.Time
	Items   []Item
}

// Item is a single feed entry; Content is HTML.
type Item struct {
	ID      string
	Title   string
	Link    string
	Content string
	Updated time.Time
}

type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Href string `xml:"href,attr"`
}

type atomEntry struct {
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Link    atomLink    `xml:"link"`
	Content atomContent `xml:"content"`
}

type atomContent struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

// writeAtom renders f as an Atom 1.0 document. self is the feed's own URL.
func writeAtom(w io.Writer, f *Feed, self string) error {
	doc := atomFeed{
		Title:   f.Title,
		ID:      cmp.Or(f.Link, self),
		Updated: f.Updated.UTC().Format(time.RFC3339),
		Links: []atomLink{
			{Href: f.Link},
			{Rel: "self", Href: self},
		},
	}
	for _, it := range f.Items {
		doc.Entries = append(doc.Entries, atomEntry{
			Title:   it.Title,
			ID:      it.ID,
			Updated: it.Updated.UTC().Format(time.RFC3339),
			Link:    atomLink{Href: it.Link},
			Content: atomContent{Type: "html", Body: it.Content},
		})
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encode atom: %w", err)
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("write trailer: %w", err)
	}
	return nil
}

// fetchHistory returns prior entries from the published feed at url,
// or nil if the feed is unreadable.
func fetchHistory(ctx context.Context, url string) []Item {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("Failed to fetch history", "url", url, "error", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil // a 404 is expected before the first publish
	}

	var doc atomFeed
	if err := xml.NewDecoder(resp.Body).Decode(&doc); err != nil {
		slog.Warn("Failed to decode history", "url", url, "error", err)
		return nil
	}
	items := make([]Item, len(doc.Entries))
	for i, e := range doc.Entries {
		updated, _ := time.Parse(time.RFC3339, e.Updated)
		items[i] = Item{ID: e.ID, Title: e.Title, Link: e.Link.Href, Content: e.Content.Body, Updated: updated}
	}
	return items
}

// mergeHistory prepends current entries to prior, dedupes by ID, caps to limit.
func mergeHistory(current, prior []Item, limit int) []Item {
	seen := make(map[string]bool, len(current))
	for _, it := range current {
		seen[it.ID] = true
	}
	merged := current
	for _, it := range prior {
		if len(merged) >= limit {
			break
		}
		if !seen[it.ID] {
			merged = append(merged, it)
		}
	}
	return merged
}
