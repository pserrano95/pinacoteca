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
CREATE TABLE IF NOT EXISTS members (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL,
  schema_version INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS credentials (
  id TEXT PRIMARY KEY,
  member_id TEXT NOT NULL,
  public_key TEXT NOT NULL,
  counter INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  aaguid TEXT NOT NULL DEFAULT '',
  backup_eligible INTEGER NOT NULL DEFAULT 0,
  backup_state INTEGER NOT NULL DEFAULT 0,
  user_present INTEGER NOT NULL DEFAULT 0,
  user_verified INTEGER NOT NULL DEFAULT 0,
  attestation_type TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  member_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS galleries (
  slug TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  wall_colour TEXT NOT NULL,
  schema_version INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS gallery_artists (
  gallery_slug TEXT NOT NULL,
  artist_slug TEXT NOT NULL,
  PRIMARY KEY (gallery_slug, artist_slug)
);
CREATE TABLE IF NOT EXISTS gallery_members (
  gallery_slug TEXT NOT NULL,
  member_id TEXT NOT NULL,
  role TEXT NOT NULL,
  PRIMARY KEY (gallery_slug, member_id)
);
`)
	return err
}

// Rebuild clears the index and reloads it from the filesystem store.
// Sessions are not reloaded: a reindex closes every one of them.
func (ix *Index) Rebuild(st *store.Store) error {
	return ix.rebuild(st, false)
}

// RebuildKeepingSessions reloads the archive and leaves login sessions in
// place. HTTP writes and process start use this so a restart or an upload
// does not sign everyone out. pinacoteca reindex uses Rebuild.
func (ix *Index) RebuildKeepingSessions(st *store.Store) error {
	return ix.rebuild(st, true)
}

func (ix *Index) rebuild(st *store.Store, keepSessions bool) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	tables := []string{"gallery_members", "gallery_artists", "galleries", "credentials", "members", "artworks", "artists"}
	if !keepSessions {
		tables = append([]string{"sessions"}, tables...)
	}
	for _, table := range tables {
		if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
			return err
		}
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

	members, err := st.ListMemberRecords()
	if err != nil {
		return err
	}
	for _, rec := range members {
		if _, err := tx.Exec(
			`INSERT INTO members (id, name, created_at, schema_version) VALUES (?, ?, ?, ?)`,
			rec.Member.ID, rec.Member.Name, rec.Member.CreatedAt.UTC().Format(time.RFC3339), rec.Member.SchemaVersion,
		); err != nil {
			return err
		}
		for _, pk := range rec.Passkeys {
			if _, err := tx.Exec(
				`INSERT INTO credentials (
					id, member_id, public_key, counter, created_at, aaguid,
					backup_eligible, backup_state, user_present, user_verified, attestation_type
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				pk.ID, rec.Member.ID, pk.PublicKey, pk.Counter, pk.CreatedAt.UTC().Format(time.RFC3339), pk.AAGUID,
				boolInt(pk.BackupEligible), boolInt(pk.BackupState), boolInt(pk.UserPresent), boolInt(pk.UserVerified), pk.AttestationType,
			); err != nil {
				return err
			}
		}
	}

	galleries, err := st.ListGalleries()
	if err != nil {
		return err
	}
	for _, g := range galleries {
		if _, err := tx.Exec(
			`INSERT INTO galleries (slug, name, wall_colour, schema_version) VALUES (?, ?, ?, ?)`,
			g.Slug, g.Name, g.WallColour, g.SchemaVersion,
		); err != nil {
			return err
		}
		for _, artistSlug := range g.Artists {
			if _, err := tx.Exec(
				`INSERT INTO gallery_artists (gallery_slug, artist_slug) VALUES (?, ?)`,
				g.Slug, artistSlug,
			); err != nil {
				return err
			}
		}
		for _, m := range g.Members {
			if _, err := tx.Exec(
				`INSERT INTO gallery_members (gallery_slug, member_id, role) VALUES (?, ?, ?)`,
				g.Slug, m.ID, m.Role,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
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

// MemberRow is an indexed member and their passkeys.
type MemberRow struct {
	ID            string
	Name          string
	CreatedAt     time.Time
	SchemaVersion int
	Passkeys      []CredentialRow
}

// CredentialRow is an indexed passkey. ID, public key and counter are the
// fields a rebuild must reproduce (D-004).
type CredentialRow struct {
	ID        string
	PublicKey string
	Counter   uint32
	CreatedAt time.Time
}

// GalleryMemberRow is one indexed membership.
type GalleryMemberRow struct {
	MemberID string
	Role     string
}

// GalleryRow is an indexed gallery, including the artists and roles it shows.
type GalleryRow struct {
	Slug          string
	Name          string
	WallColour    string
	SchemaVersion int
	Artists       []string
	Members       []GalleryMemberRow
}

// Snapshot is used by the D-004 round-trip test.
type Snapshot struct {
	Artists   []ArtistRow
	Artworks  []ArtworkRow
	Members   []MemberRow
	Galleries []GalleryRow
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
	members, err := ix.ListMembers()
	if err != nil {
		return Snapshot{}, err
	}
	galleries, err := ix.ListGalleries()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Artists: artists, Artworks: artworks, Members: members, Galleries: galleries}, nil
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
	if len(a.Members) != len(b.Members) {
		return fmt.Errorf("member count %d != %d", len(a.Members), len(b.Members))
	}
	for i := range a.Members {
		aa, bb := a.Members[i], b.Members[i]
		if aa.ID != bb.ID || aa.Name != bb.Name || !aa.CreatedAt.Equal(bb.CreatedAt) || aa.SchemaVersion != bb.SchemaVersion {
			return fmt.Errorf("member mismatch at %d: %+v vs %+v", i, aa, bb)
		}
		if len(aa.Passkeys) != len(bb.Passkeys) {
			return fmt.Errorf("member %s credential count %d != %d", aa.ID, len(aa.Passkeys), len(bb.Passkeys))
		}
		for j := range aa.Passkeys {
			ca, cb := aa.Passkeys[j], bb.Passkeys[j]
			if ca.ID != cb.ID || ca.PublicKey != cb.PublicKey || ca.Counter != cb.Counter || !ca.CreatedAt.Equal(cb.CreatedAt) {
				return fmt.Errorf("credential mismatch for %s at %d: %+v vs %+v", aa.ID, j, ca, cb)
			}
		}
	}
	if len(a.Galleries) != len(b.Galleries) {
		return fmt.Errorf("gallery count %d != %d", len(a.Galleries), len(b.Galleries))
	}
	for i := range a.Galleries {
		ga, gb := a.Galleries[i], b.Galleries[i]
		if ga.Slug != gb.Slug || ga.Name != gb.Name || ga.WallColour != gb.WallColour || ga.SchemaVersion != gb.SchemaVersion {
			return fmt.Errorf("gallery mismatch at %d: %+v vs %+v", i, ga, gb)
		}
		if !tagsEqual(ga.Artists, gb.Artists) {
			return fmt.Errorf("gallery %s artists: %v vs %v", ga.Slug, ga.Artists, gb.Artists)
		}
		if len(ga.Members) != len(gb.Members) {
			return fmt.Errorf("gallery %s member count %d != %d", ga.Slug, len(ga.Members), len(gb.Members))
		}
		for j := range ga.Members {
			if ga.Members[j].MemberID != gb.Members[j].MemberID || ga.Members[j].Role != gb.Members[j].Role {
				return fmt.Errorf("gallery %s member mismatch at %d: %+v vs %+v", ga.Slug, j, ga.Members[j], gb.Members[j])
			}
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

// ListMembers returns members and their passkeys, ordered by id.
// Credentials are loaded after the member rows are closed: the index uses a
// single SQLite connection, so a query inside rows.Next deadlocks.
func (ix *Index) ListMembers() ([]MemberRow, error) {
	rows, err := ix.db.Query(`SELECT id, name, created_at, schema_version FROM members ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []MemberRow
	for rows.Next() {
		var m MemberRow
		var created string
		if err := rows.Scan(&m.ID, &m.Name, &created, &m.SchemaVersion); err != nil {
			rows.Close()
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, created)
		if err != nil {
			rows.Close()
			return nil, err
		}
		m.CreatedAt = t
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		creds, err := ix.listCredentials(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Passkeys = creds
	}
	return out, nil
}

func (ix *Index) listCredentials(memberID string) ([]CredentialRow, error) {
	rows, err := ix.db.Query(
		`SELECT id, public_key, counter, created_at FROM credentials WHERE member_id = ? ORDER BY id`,
		memberID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CredentialRow
	for rows.Next() {
		var c CredentialRow
		var created string
		var counter int64
		if err := rows.Scan(&c.ID, &c.PublicKey, &counter, &created); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, created)
		if err != nil {
			return nil, err
		}
		c.CreatedAt = t
		c.Counter = uint32(counter)
		out = append(out, c)
	}
	return out, rows.Err()
}

// FindPasskey loads the member and credential for a credential id.
func (ix *Index) FindPasskey(credID string) (store.Member, store.Passkey, error) {
	var m store.Member
	var pk store.Passkey
	var createdM, createdC string
	var counter int64
	var be, bs, up, uv int
	err := ix.db.QueryRow(`
SELECT m.id, m.name, m.created_at, m.schema_version,
       c.id, c.public_key, c.counter, c.created_at, c.aaguid,
       c.backup_eligible, c.backup_state, c.user_present, c.user_verified, c.attestation_type
FROM credentials c
JOIN members m ON m.id = c.member_id
WHERE c.id = ?`, credID).Scan(
		&m.ID, &m.Name, &createdM, &m.SchemaVersion,
		&pk.ID, &pk.PublicKey, &counter, &createdC, &pk.AAGUID,
		&be, &bs, &up, &uv, &pk.AttestationType,
	)
	if err != nil {
		return store.Member{}, store.Passkey{}, err
	}
	m.CreatedAt, err = time.Parse(time.RFC3339, createdM)
	if err != nil {
		return store.Member{}, store.Passkey{}, err
	}
	pk.CreatedAt, err = time.Parse(time.RFC3339, createdC)
	if err != nil {
		return store.Member{}, store.Passkey{}, err
	}
	pk.Counter = uint32(counter)
	pk.BackupEligible = be != 0
	pk.BackupState = bs != 0
	pk.UserPresent = up != 0
	pk.UserVerified = uv != 0
	return m, pk, nil
}

// UpdatePasskey writes the sign counter and flags back into the index.
func (ix *Index) UpdatePasskey(pk store.Passkey) error {
	res, err := ix.db.Exec(
		`UPDATE credentials SET counter = ?, backup_eligible = ?, backup_state = ?, user_present = ?, user_verified = ? WHERE id = ?`,
		pk.Counter, boolInt(pk.BackupEligible), boolInt(pk.BackupState), boolInt(pk.UserPresent), boolInt(pk.UserVerified), pk.ID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("passkey %s not in index", pk.ID)
	}
	return nil
}

// CreateSession stores a login session. Sessions are not part of the archive.
func (ix *Index) CreateSession(id, memberID string, now, exp time.Time) error {
	_, err := ix.db.Exec(
		`INSERT INTO sessions (id, member_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		id, memberID, now.UTC().Format(time.RFC3339), exp.UTC().Format(time.RFC3339),
	)
	return err
}

// MemberFromSession returns the member for a live session.
func (ix *Index) MemberFromSession(sessionID string, now time.Time) (store.Member, error) {
	var m store.Member
	var created, expires string
	err := ix.db.QueryRow(`
SELECT m.id, m.name, m.created_at, m.schema_version, s.expires_at
FROM sessions s
JOIN members m ON m.id = s.member_id
WHERE s.id = ?`, sessionID).Scan(&m.ID, &m.Name, &created, &m.SchemaVersion, &expires)
	if err != nil {
		return store.Member{}, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return store.Member{}, err
	}
	if !now.Before(exp) {
		return store.Member{}, fmt.Errorf("session expired")
	}
	m.CreatedAt, err = time.Parse(time.RFC3339, created)
	if err != nil {
		return store.Member{}, err
	}
	return m, nil
}

// DeleteSession removes one session.
func (ix *Index) DeleteSession(id string) error {
	_, err := ix.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// ListGalleries returns galleries with their artists and members.
// Child rows are loaded after the gallery cursor is closed: the index uses
// one SQLite connection, so a query inside rows.Next deadlocks.
func (ix *Index) ListGalleries() ([]GalleryRow, error) {
	rows, err := ix.db.Query(`SELECT slug, name, wall_colour, schema_version FROM galleries ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	var out []GalleryRow
	for rows.Next() {
		var g GalleryRow
		if err := rows.Scan(&g.Slug, &g.Name, &g.WallColour, &g.SchemaVersion); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		artists, err := ix.galleryArtistSlugs(out[i].Slug)
		if err != nil {
			return nil, err
		}
		members, err := ix.galleryMembers(out[i].Slug)
		if err != nil {
			return nil, err
		}
		out[i].Artists = artists
		out[i].Members = members
	}
	return out, nil
}

func (ix *Index) galleryArtistSlugs(gallerySlug string) ([]string, error) {
	rows, err := ix.db.Query(
		`SELECT artist_slug FROM gallery_artists WHERE gallery_slug = ? ORDER BY artist_slug`,
		gallerySlug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}

func (ix *Index) galleryMembers(gallerySlug string) ([]GalleryMemberRow, error) {
	rows, err := ix.db.Query(
		`SELECT member_id, role FROM gallery_members WHERE gallery_slug = ? ORDER BY member_id`,
		gallerySlug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GalleryMemberRow{}
	for rows.Next() {
		var m GalleryMemberRow
		if err := rows.Scan(&m.MemberID, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CanSeeArtist reports whether the member belongs to a gallery that lists the artist.
func (ix *Index) CanSeeArtist(memberID, artistSlug string) (bool, error) {
	var n int
	err := ix.db.QueryRow(`
SELECT COUNT(*) FROM gallery_members gm
JOIN gallery_artists ga ON ga.gallery_slug = gm.gallery_slug
WHERE gm.member_id = ? AND ga.artist_slug = ?`, memberID, artistSlug).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CanUploadArtist reports whether the member is owner or contributor of a gallery that lists the artist.
func (ix *Index) CanUploadArtist(memberID, artistSlug string) (bool, error) {
	var n int
	err := ix.db.QueryRow(`
SELECT COUNT(*) FROM gallery_members gm
JOIN gallery_artists ga ON ga.gallery_slug = gm.gallery_slug
WHERE gm.member_id = ? AND ga.artist_slug = ? AND gm.role IN (?, ?)`,
		memberID, artistSlug, store.RoleOwner, store.RoleContributor).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// MemberCanUpload reports whether the member is owner or contributor of any gallery.
func (ix *Index) MemberCanUpload(memberID string) (bool, error) {
	var n int
	err := ix.db.QueryRow(
		`SELECT COUNT(*) FROM gallery_members WHERE member_id = ? AND role IN (?, ?)`,
		memberID, store.RoleOwner, store.RoleContributor,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// VisibleArtistSlugs is the set of artists the member can see.
func (ix *Index) VisibleArtistSlugs(memberID string) (map[string]struct{}, error) {
	return ix.artistSlugsForMember(memberID, false)
}

// UploadableArtistSlugs is the set of artists the member may upload for.
func (ix *Index) UploadableArtistSlugs(memberID string) (map[string]struct{}, error) {
	return ix.artistSlugsForMember(memberID, true)
}

func (ix *Index) artistSlugsForMember(memberID string, uploadOnly bool) (map[string]struct{}, error) {
	q := `
SELECT DISTINCT ga.artist_slug
FROM gallery_artists ga
JOIN gallery_members gm ON gm.gallery_slug = ga.gallery_slug
WHERE gm.member_id = ?`
	args := []any{memberID}
	if uploadOnly {
		q += ` AND gm.role IN (?, ?)`
		args = append(args, store.RoleOwner, store.RoleContributor)
	}
	rows, err := ix.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out[slug] = struct{}{}
	}
	return out, rows.Err()
}
