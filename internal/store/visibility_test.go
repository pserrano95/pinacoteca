package store_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/store"
)

func visibilityFixture(t *testing.T) (*store.Store, string, *store.ArtworkRecord) {
	t.Helper()
	st := store.New(t.TempDir())
	m, err := st.CreateMember("Parent", []store.Passkey{{ID: "visibility-key", PublicKey: "visibility-public-key", CreatedAt: time.Now()}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateArtistWithGuardian("Ada", time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	art, err := st.CreateArtwork(store.CreateArtworkInput{ArtistSlug: a.Slug, Title: "Sun", Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Description: "A gift", Tags: []string{"sun"}, GiftedTo: "Grandma", ImageBytes: pngBytes(t)})
	if err != nil {
		t.Fatal(err)
	}
	return st, m.ID, art
}

func TestLegacyArtworkWithoutHiddenLoadsVisible(t *testing.T) {
	st, _, art := visibilityFixture(t)
	raw, err := os.ReadFile(filepath.Join(art.Dir, store.ArtworkFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"hidden"`)) {
		t.Fatal("fixture is not legacy metadata")
	}
	got, err := st.LoadArtwork(art.ArtistSlug, art.Folder)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Hidden || got.Meta.SchemaVersion != 1 {
		t.Fatalf("legacy metadata: %+v", got.Meta)
	}
}

func TestArtworkVisibilityPreservesMetadataAndOriginal(t *testing.T) {
	st, id, art := visibilityFixture(t)
	before, err := os.ReadFile(art.OriginalPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []bool{true, false} {
		if err := st.SetArtworkHidden(art.ArtistSlug, art.Folder, id, hidden); err != nil {
			t.Fatal(err)
		}
		got, err := st.LoadArtwork(art.ArtistSlug, art.Folder)
		if err != nil {
			t.Fatal(err)
		}
		want := art.Meta
		want.Hidden = hidden
		if !reflect.DeepEqual(got.Meta, want) {
			t.Fatalf("metadata changed: %+v want %+v", got.Meta, want)
		}
		original, err := os.ReadFile(art.OriginalPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, original) {
			t.Fatal("original modified")
		}
		raw, err := os.ReadFile(filepath.Join(art.Dir, store.ArtworkFile))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(`"hidden"`)) != hidden {
			t.Fatal("hidden must be omitted when visible")
		}
	}
}

func TestArtworkVisibilityRejectsOtherGuardianAndTraversalWithoutWrites(t *testing.T) {
	st, id, art := visibilityFixture(t)
	path := filepath.Join(art.Dir, store.ArtworkFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ artist, folder, member string }{{art.ArtistSlug, art.Folder, strings.Repeat("a", 32)}, {art.ArtistSlug, "../" + art.Folder, id}, {"../ada", art.Folder, id}, {art.ArtistSlug, "..", id}} {
		if err := st.SetArtworkHidden(tc.artist, tc.folder, tc.member, true); err == nil {
			t.Fatalf("accepted invalid mutation: %+v", tc)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected update wrote metadata")
	}
}

func TestConcurrentArtworkVisibilityAcrossStores(t *testing.T) {
	st, id, art := visibilityFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(hidden bool) {
			defer wg.Done()
			other := store.New(st.DataDir)
			if err := other.SetArtworkHidden(art.ArtistSlug, art.Folder, id, hidden); err != nil {
				t.Error(err)
			}
		}(i%2 == 0)
	}
	wg.Wait()
	got, err := st.LoadArtwork(art.ArtistSlug, art.Folder)
	if err != nil {
		t.Fatal(err)
	}
	got.Meta.Hidden = false
	if !reflect.DeepEqual(got.Meta, art.Meta) {
		t.Fatal("concurrent updates corrupted metadata")
	}
	entries, err := os.ReadDir(art.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
}
