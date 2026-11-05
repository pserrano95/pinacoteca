package store_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/imageutil"
	"github.com/pserrano95/pinacoteca/internal/store"
)

func TestCropArchiveOriginalAndRegeneration(t *testing.T) {
	st := store.New(t.TempDir())
	a, err := st.CreateArtist("Ada", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	artistPath := filepath.Join(st.ArtistsRoot(), a.Slug, store.ArtistFile)
	artistJSON, err := os.ReadFile(artistPath)
	if err != nil {
		t.Fatal(err)
	}
	original := pngBytes(t)
	for _, crop := range []*imageutil.Crop{nil, {QuarterTurns: 1, StraightenDeg: 10, Rect: imageutil.Rect{X: .1, Y: .2, W: .7, H: .6}}} {
		rec, err := st.CreateArtwork(store.CreateArtworkInput{ArtistSlug: a.Slug, Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), ImageBytes: original, Crop: crop})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(rec.OriginalPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, original) {
			t.Fatal("original is not byte-for-byte identical")
		}
		metadata, _ := os.ReadFile(filepath.Join(rec.Dir, store.ArtworkFile))
		if bytes.Contains(metadata, []byte(`"crop"`)) != (crop != nil) {
			t.Fatal("crop omission incorrect")
		}
		var old store.Artwork
		if err := json.Unmarshal(metadata, &old); err != nil || old.SchemaVersion != 1 {
			t.Fatal("legacy/schema failure", err)
		}
		if crop != nil && rec.DisplayPath == "" {
			t.Fatal("no display")
		}
		files := map[string][]byte{}
		dir := filepath.Join(rec.Dir, store.DerivedDir)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			files[e.Name()], err = os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
		}
		if err = os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		// Regeneration must not depend on artist metadata or any derived sidecar.
		if err = os.Remove(artistPath); err != nil {
			t.Fatal(err)
		}
		if err = st.RegenerateDerived(a.Slug, rec.Folder); err != nil {
			t.Fatal(err)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || !bytes.Equal(want, got) {
				t.Fatalf("%s regeneration differs: %v", name, err)
			}
		}
		if err = os.WriteFile(artistPath, artistJSON, 0644); err != nil {
			t.Fatal(err)
		}
		loaded, err := st.LoadArtwork(a.Slug, rec.Folder)
		if err != nil {
			t.Fatal(err)
		}
		if (loaded.Meta.Crop != nil) != (crop != nil) {
			t.Fatal("crop lost on load")
		}
	}
}

func TestCropStoreRejectsBeforeWriting(t *testing.T) {
	st := store.New(t.TempDir())
	a, err := st.CreateArtist("Ada", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadDir(filepath.Join(st.ArtistsRoot(), a.Slug))
	_, err = st.CreateArtwork(store.CreateArtworkInput{ArtistSlug: a.Slug, Date: time.Now(), ImageBytes: pngBytes(t), Crop: &imageutil.Crop{QuarterTurns: 4, Rect: imageutil.Rect{W: 1, H: 1}}})
	if err == nil {
		t.Fatal("accepted invalid crop")
	}
	after, _ := os.ReadDir(filepath.Join(st.ArtistsRoot(), a.Slug))
	if len(before) != len(after) {
		t.Fatal("invalid crop wrote artwork")
	}
}
