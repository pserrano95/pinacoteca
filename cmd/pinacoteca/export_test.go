package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/store"
)

func TestExportCommand(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	artist, err := st.CreateArtist("Ada", time.Date(2018, 5, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "export-ada")
	if err := runExport([]string{"--data", dir, "--artist", artist.Slug, "--out", outDir, "--lang", "es"}); err != nil {
		t.Fatalf("runExport: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "index.html")); err != nil {
		t.Fatalf("index.html missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "artist.json")); err != nil {
		t.Fatalf("artist.json missing: %v", err)
	}

	// Missing --artist
	if err := runExport([]string{"--data", dir, "--out", outDir}); err == nil {
		t.Fatal("expected error when --artist is missing")
	}

	// Missing --out
	if err := runExport([]string{"--data", dir, "--artist", artist.Slug}); err == nil {
		t.Fatal("expected error when --out is missing")
	}
}
