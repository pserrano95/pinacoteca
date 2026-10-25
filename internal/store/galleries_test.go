package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/store"
)

func TestGalleryClosedLists(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	g, err := st.CreateGallery("Abuelos", "")
	if err != nil {
		t.Fatal(err)
	}
	if g.Slug != "abuelos" || g.WallColour != store.WallColourDefault || g.SchemaVersion != store.SchemaVersion {
		t.Fatalf("gallery: %+v", g)
	}
	if _, err := st.CreateGallery("Otra", "neon"); err == nil {
		t.Fatal("unknown wall colour was accepted")
	}
	artist, err := st.CreateArtist("Lucía", time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateMember("Ana", []store.Passkey{{
		ID: "cred-ana", PublicKey: "pk-ana", CreatedAt: time.Now().UTC(),
	}}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(g.Slug, member.ID, "curator"); err == nil {
		t.Fatal("unknown role was accepted")
	}
	if err := st.AddGalleryMember(g.Slug, member.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(g.Slug, member.ID, store.RoleViewer); err == nil {
		t.Fatal("duplicate member was accepted")
	}
	if err := st.AddGalleryArtist(g.Slug, artist.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(g.Slug, artist.Slug); err == nil {
		t.Fatal("duplicate artist was accepted")
	}
	loaded, err := st.LoadGallery(g.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Artists) != 1 || loaded.Artists[0] != artist.Slug {
		t.Fatalf("artists: %+v", loaded.Artists)
	}
	if len(loaded.Members) != 1 || loaded.Members[0].Role != store.RoleOwner {
		t.Fatalf("members: %+v", loaded.Members)
	}
}

func TestArtistInTwoGalleriesOneFolder(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	artist, err := st.CreateArtist("Lucía", time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: artist.Slug,
		Title:      "Sun",
		Date:       time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: pngBytes(t),
	}); err != nil {
		t.Fatal(err)
	}
	home, err := st.CreateGallery("Familia", "")
	if err != nil {
		t.Fatal(err)
	}
	cousins, err := st.CreateGallery("Primos", "pale-sage")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(home.Slug, artist.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(cousins.Slug, artist.Slug); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(st.ArtistsRoot())
	if err != nil {
		t.Fatal(err)
	}
	var dirs int
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		}
	}
	if dirs != 1 {
		t.Fatalf("artist directories: got %d, want 1", dirs)
	}
	if _, err := os.Stat(filepath.Join(st.ArtistsRoot(), artist.Slug)); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{home.Slug, cousins.Slug} {
		g, err := st.LoadGallery(slug)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Artists) != 1 || g.Artists[0] != artist.Slug {
			t.Fatalf("gallery %s artists: %+v", slug, g.Artists)
		}
	}
	recs, err := st.WalkArtworks()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("artworks on disk: got %d, want 1", len(recs))
	}
}
