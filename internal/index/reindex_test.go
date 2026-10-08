package index_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 12, 12))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestReindexRoundTrip is the D-004 guardian: two artists, three artworks,
// delete the database, reindex, and the gallery must match including gifted_to and ages.
func TestReindexRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	a1, err := st.CreateArtist("Lucía", time.Date(2020, 6, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	a2, err := st.CreateArtist("Mateo", time.Date(2018, 1, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	_, err = st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug:  a1.Slug,
		Title:       "Rainbow",
		Date:        time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
		Description: "Birthday gift",
		Tags:        []string{"color"},
		GiftedTo:    "Tía Ana",
		ImageBytes:  pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a1.Slug,
		Title:      "Dog",
		Date:       time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		GiftedTo:   "Papá",
		ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a2.Slug,
		Date:       time.Date(2022, 8, 8, 0, 0, 0, 0, time.UTC),
		ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	before, err := ix.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Artists) != 2 || len(before.Artworks) != 3 {
		t.Fatalf("setup: got %d artists %d artworks", len(before.Artists), len(before.Artworks))
	}
	// Spot-check ages and gifted_to are in the index.
	var foundGift bool
	for _, aw := range before.Artworks {
		if aw.GiftedTo == "Tía Ana" {
			foundGift = true
			if aw.AgeYears != 4 {
				t.Fatalf("Rainbow age: got %d want 4", aw.AgeYears)
			}
		}
	}
	if !foundGift {
		t.Fatal("gifted_to not present in snapshot before delete")
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	dbPath := index.DBPath(dir)
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	// Also remove WAL leftovers if any.
	for _, suf := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suf)
	}

	ix2, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ix2.Close()
	if err := ix2.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	after, err := ix2.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := index.EqualSnapshots(before, after); err != nil {
		t.Fatalf("reindex did not reproduce gallery: %v", err)
	}
}

func TestEmptyDataDirFirstStart(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	snap, err := ix.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Artists) != 0 || len(snap.Artworks) != 0 {
		t.Fatalf("expected empty index, got %+v", snap)
	}
	if _, err := os.Stat(filepath.Join(dir, "artists")); err != nil {
		t.Fatal(err)
	}
}

func TestReindexWithoutExistingDB(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, _ := st.CreateArtist("Only", time.Date(2019, 9, 9, 0, 0, 0, 0, time.UTC))
	_, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug,
		Title:      "One",
		Date:       time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
		GiftedTo:   "Someone",
		ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	// No DB yet — Open creates it, Rebuild fills from disk.
	if _, err := os.Stat(index.DBPath(dir)); !os.IsNotExist(err) {
		// Open hasn't been called; ensure no leftover.
		_ = os.Remove(index.DBPath(dir))
	}
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	snap, err := ix.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Artworks) != 1 || snap.Artworks[0].GiftedTo != "Someone" {
		t.Fatalf("unexpected snapshot: %+v", snap.Artworks)
	}
}
