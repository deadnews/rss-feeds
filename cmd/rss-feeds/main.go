// Package main provides rss-feeds, an Atom feed generator with pluggable sources.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// maxEntries caps how many past entries each feed retains.
const maxEntries = 30

// baseURL is where feeds are served; used for self-links and history fetches.
var baseURL = "https://deadnews.github.io/rss-feeds"

// sources lists the feeds to generate.
var sources = []Source{
	HackerNewsTop(20),
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	outDir := flag.String("out", "dist", "output directory")
	flag.Parse()

	if err := run(*outDir); err != nil {
		slog.Error("Fatal error", "error", err)
		os.Exit(1)
	}
}

// run generates every source into outDir, continuing past individual failures.
func run(outDir string) error {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var failed int
	for _, src := range sources {
		if err := generate(ctx, outDir, src); err != nil {
			slog.Error("Failed to generate feed", "source", src.Name(), "error", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d sources failed", failed, len(sources))
	}
	return nil
}

// generate fetches one source, merges prior history, and writes its Atom file.
func generate(ctx context.Context, outDir string, src Source) error {
	feed, err := src.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	self := baseURL + "/" + src.Name() + ".xml"
	feed.Items = mergeHistory(feed.Items, fetchHistory(ctx, self), maxEntries)

	var buf bytes.Buffer
	if err := writeAtom(&buf, feed, self); err != nil {
		return err
	}

	path := filepath.Join(outDir, src.Name()+".xml")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	slog.Info("Wrote feed", "path", path, "entries", len(feed.Items))
	return nil
}
