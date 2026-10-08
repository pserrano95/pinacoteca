package store_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/store"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCreateArtistEmptyGallery(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateArtist("Lucía", time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	recs, err := st.WalkArtworks()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 artworks, got %d", len(recs))
	}
	if _, err := os.Stat(filepath.Join(dir, "artists", a.Slug, "artist.json")); err != nil {
		t.Fatal(err)
	}
}

func TestArtworkFullAndMinimal(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, err := st.CreateArtist("Maya", time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	full, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug:  a.Slug,
		Title:       "Sunset",
		Date:        time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		Description: "For grandma",
		Tags:        []string{"sun", "warm"},
		GiftedTo:    "Abuela",
		ImageBytes:  pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if full.Meta.GiftedTo != "Abuela" || full.Meta.Description == "" || len(full.Meta.Tags) != 2 {
		t.Fatalf("full fields not stored: %+v", full.Meta)
	}
	min, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug,
		Date:       time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if min.Meta.Title != "" || min.Meta.GiftedTo != "" {
		t.Fatalf("minimal should omit optional fields: %+v", min.Meta)
	}
}

func TestSameDaySameTitleNoClobber(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, _ := st.CreateArtist("Sam", time.Date(2018, 5, 5, 0, 0, 0, 0, time.UTC))
	day := time.Date(2023, 8, 8, 0, 0, 0, 0, time.UTC)
	r1, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug, Title: "Cat", Date: day, ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug, Title: "Cat", Date: day, ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Folder == r2.Folder {
		t.Fatalf("folders collided: %s", r1.Folder)
	}
	if _, err := os.Stat(r1.OriginalPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r2.OriginalPath); err != nil {
		t.Fatal(err)
	}
}

func TestRejectNonImageLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, _ := st.CreateArtist("Sam", time.Date(2018, 5, 5, 0, 0, 0, 0, time.UTC))
	before, _ := os.ReadDir(filepath.Join(dir, "artists", a.Slug))
	_, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug,
		Title:      "Nope",
		Date:       time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: []byte("not an image at all"),
	})
	if err == nil {
		t.Fatal("expected rejection")
	}
	after, _ := os.ReadDir(filepath.Join(dir, "artists", a.Slug))
	if len(after) != len(before) {
		t.Fatalf("leftover on disk: before %d after %d", len(before), len(after))
	}
}

func TestRejectBeforeBirth(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, _ := st.CreateArtist("Sam", time.Date(2018, 5, 5, 0, 0, 0, 0, time.UTC))
	_, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug,
		Date:       time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: pngBytes(t),
	})
	if err == nil {
		t.Fatal("expected before-birth rejection")
	}
}

func TestOriginalBytesUnchanged(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, _ := st.CreateArtist("Sam", time.Date(2018, 5, 5, 0, 0, 0, 0, time.UTC))
	raw := pngBytes(t)
	rec, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug,
		Date:       time.Date(2022, 2, 2, 0, 0, 0, 0, time.UTC),
		ImageBytes: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(rec.OriginalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("original was re-encoded")
	}
}
