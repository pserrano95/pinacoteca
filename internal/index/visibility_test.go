package index_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
)

func TestArtworkVisibilityOpensAndReindexesLegacyDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, store.IndexDBName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Exactly the artworks columns in the pre-visibility schema. Opening this
	// populated table must not depend on CREATE TABLE adding a hidden column.
	_, err = db.Exec(`CREATE TABLE artists (slug TEXT PRIMARY KEY, name TEXT NOT NULL, birth_date TEXT NOT NULL, schema_version INTEGER NOT NULL);
 CREATE TABLE artworks (
 artist_slug TEXT NOT NULL, folder TEXT NOT NULL, title TEXT NOT NULL,
 date TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', tags_json TEXT NOT NULL DEFAULT '[]',
 gifted_to TEXT NOT NULL DEFAULT '', original_name TEXT NOT NULL, age_years INTEGER NOT NULL,
 schema_version INTEGER NOT NULL, PRIMARY KEY (artist_slug,folder),
 FOREIGN KEY (artist_slug) REFERENCES artists(slug));
 INSERT INTO artists VALUES ('old', 'Old', '2018-01-01', 1);
 INSERT INTO artworks VALUES ('old', 'old-sun', 'OldSun', '2024-01-01', '', '[]', '', 'original.png', 6, 1);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	old, err := ix.GetArtwork("old", "old-sun")
	if err != nil {
		t.Fatal(err)
	}
	if old.Hidden {
		t.Fatal("old indexed work became hidden")
	}
	st := store.New(dir)
	m, err := st.CreateMember("Parent", []store.Passkey{{ID: "visibility-key", PublicKey: "visibility-public-key", CreatedAt: time.Now()}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateArtistWithGuardian("Ada", time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	art, err := st.CreateArtwork(store.CreateArtworkInput{ArtistSlug: a.Slug, Title: "Sun", Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), ImageBytes: pngBytes(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetArtworkHidden(a.Slug, art.Folder, m.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	row, err := ix.GetArtwork(a.Slug, art.Folder)
	if err != nil {
		t.Fatal(err)
	}
	if !row.Hidden {
		t.Fatal("legacy database did not index hidden artwork")
	}
	list, err := ix.ListArtworksByArtist(a.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].Hidden {
		t.Fatalf("artist list: %+v", list)
	}
}
