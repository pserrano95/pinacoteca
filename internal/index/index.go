package index

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/pserrano95/pinacoteca/internal/store"
)

// Index is a rebuildable SQLite cache of the filesystem archive.
type Index struct {
	db   *sql.DB
	path string
}

func DBPath(dataDir string) string {
	return filepath.Join(dataDir, store.IndexDBName)
}

func Open(dataDir string) (*Index, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	path := DBPath(dataDir)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ix := &Index{db: db, path: path}
	if err := ix.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return ix, nil
}

func (ix *Index) Close() error {
	return ix.db.Close()
}

func (ix *Index) Path() string { return ix.path }

func (ix *Index) migrate() error {
	_, err := ix.db.Exec(`
CREATE TABLE IF NOT EXISTS artists (
  slug TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  birth_date TEXT NOT NULL,
  schema_version INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS artworks (
  artist_slug TEXT NOT NULL,
  folder TEXT NOT NULL,
  title TEXT NOT NULL,
  date TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  tags_json TEXT NOT NULL DEFAULT '[]',
  gifted_to TEXT NOT NULL DEFAULT '',
  original_name TEXT NOT NULL,
  age_years INTEGER NOT NULL,
  schema_version INTEGER NOT NULL,
  PRIMARY KEY (artist_slug, folder),
  FOREIGN KEY (artist_slug) REFERENCES artists(slug)
);
`)
	return err
}

// Rebuild clears the index and reloads it from the filesystem store.
func (ix *Index) Rebuild(st *store.Store) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM artworks`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM artists`); err != nil {
		return err
	}

	artists, err := st.ListArtists()
	if err != nil {
		return err
	}
	for _, a := range artists {
		if _, err := tx.Exec(
			`INSERT INTO artists (slug, name, birth_date, schema_version) VALUES (?, ?, ?, ?)`,
			a.Slug, a.Name, a.BirthDate.Format("2006-01-02"), a.SchemaVersion,
		); err != nil {
			return err
		}
	}

	recs, err := st.WalkArtworks()
	if err != nil {
		return err
	}
	for _, r := range recs {
		tagsJSON, err := json.Marshal(r.Meta.Tags)
		if err != nil {
			return err
		}
		if tagsJSON == nil {
			tagsJSON = []byte("[]")
		}
		if _, err := tx.Exec(
			`INSERT INTO artworks (
				artist_slug, folder, title, date, description, tags_json, gifted_to,
				original_name, age_years, schema_version
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ArtistSlug, r.Folder, r.Meta.Title, r.Meta.Date.Format("2006-01-02"),
			r.Meta.Description, string(tagsJSON), r.Meta.GiftedTo,
			r.OriginalName, r.AgeYears, r.Meta.SchemaVersion,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ArtistRow is an indexed artist.
type ArtistRow struct {
	Slug          string
	Name          string
	BirthDate     time.Time
	SchemaVersion int
}

// ArtworkRow is an indexed artwork with computed age.
type ArtworkRow struct {
	ArtistSlug    string
	ArtistName    string
	Folder        string
	Title         string
	Date          time.Time
	Description   string
	Tags          []string
	GiftedTo      string
	OriginalName  string
	AgeYears      int
	SchemaVersion int
}

func (ix *Index) ListArtists() ([]ArtistRow, error) {
	rows, err := ix.db.Query(`SELECT slug, name, birth_date, schema_version FROM artists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArtistRow
	for rows.Next() {
		var r ArtistRow
		var birth string
		if err := rows.Scan(&r.Slug, &r.Name, &birth, &r.SchemaVersion); err != nil {
			return nil, err
		}
		t, err := time.Parse("2006-01-02", birth)
		if err != nil {
			return nil, err
		}
		r.BirthDate = t
		out = append(out, r)
	}
	return out, rows.Err()
}

func (ix *Index) GetArtist(slug string) (*ArtistRow, error) {
	var r ArtistRow
	var birth string
	err := ix.db.QueryRow(
		`SELECT slug, name, birth_date, schema_version FROM artists WHERE slug = ?`, slug,
	).Scan(&r.Slug, &r.Name, &birth, &r.SchemaVersion)
	if err != nil {
		return nil, err
	}
	t, err := time.Parse("2006-01-02", birth)
	if err != nil {
		return nil, err
	}
	r.BirthDate = t
	return &r, nil
}

func (ix *Index) ListArtworks() ([]ArtworkRow, error) {
	rows, err := ix.db.Query(`
SELECT a.artist_slug, ar.name, a.folder, a.title, a.date, a.description, a.tags_json,
       a.gifted_to, a.original_name, a.age_years, a.schema_version
FROM artworks a
JOIN artists ar ON ar.slug = a.artist_slug
ORDER BY a.date DESC, a.folder ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtworkRows(rows)
}

func (ix *Index) ListArtworksByArtist(artistSlug string) ([]ArtworkRow, error) {
	rows, err := ix.db.Query(`
SELECT a.artist_slug, ar.name, a.folder, a.title, a.date, a.description, a.tags_json,
       a.gifted_to, a.original_name, a.age_years, a.schema_version
FROM artworks a
JOIN artists ar ON ar.slug = a.artist_slug
WHERE a.artist_slug = ?
ORDER BY a.date DESC, a.folder ASC`, artistSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtworkRows(rows)
}

func (ix *Index) GetArtwork(artistSlug, folder string) (*ArtworkRow, error) {
	row := ix.db.QueryRow(`
SELECT a.artist_slug, ar.name, a.folder, a.title, a.date, a.description, a.tags_json,
       a.gifted_to, a.original_name, a.age_years, a.schema_version
FROM artworks a
JOIN artists ar ON ar.slug = a.artist_slug
WHERE a.artist_slug = ? AND a.folder = ?`, artistSlug, folder)
	list, err := scanArtworkRow(row)
	if err != nil {
		return nil, err
	}
	return &list, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanArtworkRows(rows *sql.Rows) ([]ArtworkRow, error) {
	var out []ArtworkRow
	for rows.Next() {
		r, err := scanArtworkRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanArtworkRow(row scannable) (ArtworkRow, error) {
	var r ArtworkRow
	var dateStr, tagsJSON string
	if err := row.Scan(
		&r.ArtistSlug, &r.ArtistName, &r.Folder, &r.Title, &dateStr, &r.Description, &tagsJSON,
		&r.GiftedTo, &r.OriginalName, &r.AgeYears, &r.SchemaVersion,
	); err != nil {
		return r, err
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return r, err
	}
	r.Date = t
	if tagsJSON == "" {
		tagsJSON = "[]"
	}
	if err := json.Unmarshal([]byte(tagsJSON), &r.Tags); err != nil {
		return r, fmt.Errorf("tags: %w", err)
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return r, nil
}

// Snapshot is used by the D-004 round-trip test.
type Snapshot struct {
	Artists  []ArtistRow
	Artworks []ArtworkRow
}

func (ix *Index) Snapshot() (Snapshot, error) {
	artists, err := ix.ListArtists()
	if err != nil {
		return Snapshot{}, err
	}
	artworks, err := ix.ListArtworks()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Artists: artists, Artworks: artworks}, nil
}

// EqualSnapshots compares gallery data including gifted_to and ages (D-004 guardian).
func EqualSnapshots(a, b Snapshot) error {
	if len(a.Artists) != len(b.Artists) {
		return fmt.Errorf("artist count %d != %d", len(a.Artists), len(b.Artists))
	}
	for i := range a.Artists {
		aa, bb := a.Artists[i], b.Artists[i]
		if aa.Slug != bb.Slug || aa.Name != bb.Name ||
			!aa.BirthDate.Equal(bb.BirthDate) || aa.SchemaVersion != bb.SchemaVersion {
			return fmt.Errorf("artist mismatch at %d: %+v vs %+v", i, aa, bb)
		}
	}
	if len(a.Artworks) != len(b.Artworks) {
		return fmt.Errorf("artwork count %d != %d", len(a.Artworks), len(b.Artworks))
	}
	for i := range a.Artworks {
		aa, bb := a.Artworks[i], b.Artworks[i]
		if aa.ArtistSlug != bb.ArtistSlug || aa.Folder != bb.Folder ||
			aa.Title != bb.Title || !aa.Date.Equal(bb.Date) ||
			aa.Description != bb.Description || aa.GiftedTo != bb.GiftedTo ||
			aa.AgeYears != bb.AgeYears || aa.SchemaVersion != bb.SchemaVersion ||
			aa.OriginalName != bb.OriginalName || !tagsEqual(aa.Tags, bb.Tags) {
			return fmt.Errorf("artwork mismatch at %d: %+v vs %+v", i, aa, bb)
		}
	}
	return nil
}

func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// FormatTags joins tags for display helpers.
func FormatTags(tags []string) string {
	return strings.Join(tags, ", ")
}
