package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pserrano95/pinacoteca/internal/age"
	"github.com/pserrano95/pinacoteca/internal/imageutil"
	"github.com/pserrano95/pinacoteca/internal/slug"
)

// Store is the filesystem archive under DataDir.
type Store struct {
	DataDir string
}

func New(dataDir string) *Store {
	return &Store{DataDir: dataDir}
}

func (s *Store) ArtistsRoot() string {
	return filepath.Join(s.DataDir, ArtistsDir)
}

func (s *Store) GalleriesRoot() string {
	return filepath.Join(s.DataDir, GalleriesDir)
}

func (s *Store) MembersRoot() string {
	return filepath.Join(s.DataDir, MembersDir)
}

func (s *Store) InvitesRoot() string {
	return filepath.Join(s.DataDir, InvitesDir)
}

func (s *Store) EnsureLayout() error {
	for _, dir := range []string{s.ArtistsRoot(), s.GalleriesRoot(), s.MembersRoot(), s.InvitesRoot()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// CreateArtist writes artists/<slug>/artist.json. Slug collisions get a numeric suffix.
func (s *Store) CreateArtist(name string, birthDate time.Time) (*Artist, error) {
	return s.createArtist(name, birthDate, nil)
}

func (s *Store) createArtist(name string, birthDate time.Time, guardians []string) (*Artist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("artist name is required")
	}
	if birthDate.IsZero() {
		return nil, fmt.Errorf("birth date is required")
	}
	base := slug.Make(name)
	artistSlug := base
	for i := 2; ; i++ {
		dir := filepath.Join(s.ArtistsRoot(), artistSlug)
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			break
		}
		artistSlug = fmt.Sprintf("%s-%d", base, i)
	}
	a := &Artist{
		Guardians:     guardians,
		SchemaVersion: SchemaVersion,
		Name:          name,
		BirthDate:     NewDate(birthDate),
		Slug:          artistSlug,
	}
	dir := filepath.Join(s.ArtistsRoot(), artistSlug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(dir, ArtistFile), a); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return a, nil
}

func (s *Store) LoadArtist(artistSlug string) (*Artist, error) {
	if err := safeSlug(artistSlug); err != nil {
		return nil, err
	}
	path := filepath.Join(s.ArtistsRoot(), artistSlug, ArtistFile)
	var a Artist
	if err := readJSON(path, &a); err != nil {
		return nil, err
	}
	if a.Slug != artistSlug {
		return nil, fmt.Errorf("artist slug does not match directory")
	}
	if err := validateArtistPermissions(a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) ListArtists() ([]Artist, error) {
	entries, err := os.ReadDir(s.ArtistsRoot())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Artist
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.ArtistsRoot(), e.Name(), ArtistFile)); errors.Is(err, os.ErrNotExist) {
			continue
		}
		a, err := s.LoadArtist(e.Name())
		if err != nil {
			return nil, fmt.Errorf("artist %s: %w", e.Name(), err)
		}
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateArtworkInput is the upload payload (original bytes unchanged on disk).
type CreateArtworkInput struct {
	Crop        *imageutil.Crop
	ArtistSlug  string
	Title       string
	Date        time.Time
	Description string
	Tags        []string
	GiftedTo    string
	ImageBytes  []byte
	UploadID    string
}

// ErrInvalidUploadID is a non-empty upload id that is not 32 lowercase hex characters.
var ErrInvalidUploadID = errors.New("invalid upload_id")

// ValidUploadID reports whether id is empty or 32 lowercase hex characters.
// Empty means "no idempotency key", which is the upload the HTML form sends.
func ValidUploadID(id string) bool {
	if id == "" {
		return true
	}
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Artist uploads share one process-wide lock per directory, same shape as
// lockGallery: two Store values on the same data dir exclude each other.
var (
	artistLocksMu sync.Mutex
	artistLocks   = map[string]*sync.Mutex{}
)

func lockArtist(path string) func() {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	} else {
		path = filepath.Clean(path)
	}
	artistLocksMu.Lock()
	mu, ok := artistLocks[path]
	if !ok {
		mu = &sync.Mutex{}
		artistLocks[path] = mu
	}
	artistLocksMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// CreateArtwork validates, writes original + artwork.json + derived thumb, and returns the record.
// On any failure after creating the folder, the folder is removed so nothing is left on disk.
// A non-empty UploadID that this artist already has returns that artwork and writes nothing.
func (s *Store) CreateArtwork(in CreateArtworkInput) (*ArtworkRecord, error) {
	if !ValidUploadID(in.UploadID) {
		return nil, ErrInvalidUploadID
	}
	if in.Crop != nil {
		if err := in.Crop.Validate(); err != nil {
			return nil, err
		}
	}
	artist, err := s.LoadArtist(in.ArtistSlug)
	if err != nil {
		return nil, fmt.Errorf("artist: %w", err)
	}
	if in.Date.IsZero() {
		return nil, fmt.Errorf("artwork date is required")
	}
	artworkDate := NewDate(in.Date)
	if _, err := age.At(artist.BirthDate.Time, artworkDate.Time); err != nil {
		return nil, err
	}
	ext, err := imageutil.DetectExt(in.ImageBytes)
	if err != nil {
		return nil, err
	}

	unlock := lockArtist(filepath.Join(s.ArtistsRoot(), artist.Slug))
	defer unlock()
	if in.UploadID != "" {
		existing, err := s.findByUploadID(*artist, in.UploadID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			existing.Duplicate = true
			return existing, nil
		}
	}

	title := strings.TrimSpace(in.Title)
	baseSlug := slug.Make(title)
	folderBase := artworkDate.Format("2006-01-02") + "-" + baseSlug
	folder := folderBase
	artistDir := filepath.Join(s.ArtistsRoot(), artist.Slug)
	for i := 2; ; i++ {
		candidate := filepath.Join(artistDir, folder)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			break
		}
		folder = fmt.Sprintf("%s-%d", folderBase, i)
	}
	dir := filepath.Join(artistDir, folder)
	if err := os.MkdirAll(filepath.Join(dir, DerivedDir), 0o755); err != nil {
		return nil, err
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
		}
	}()

	originalName := "original" + ext
	originalPath := filepath.Join(dir, originalName)
	if err := imageutil.WriteOriginal(originalPath, in.ImageBytes); err != nil {
		return nil, err
	}

	meta := Artwork{
		Crop:          in.Crop,
		SchemaVersion: SchemaVersion,
		UploadID:      in.UploadID,
		Title:         title,
		Date:          artworkDate,
		Description:   strings.TrimSpace(in.Description),
		Tags:          cleanTags(in.Tags),
		GiftedTo:      strings.TrimSpace(in.GiftedTo),
	}
	if err := writeJSON(filepath.Join(dir, ArtworkFile), &meta); err != nil {
		return nil, err
	}

	thumbPath := filepath.Join(dir, DerivedDir, ThumbName)
	if err := s.regenerateDerived(originalPath, meta); err != nil {
		return nil, err
	}
	displayPath := ""
	if meta.Crop != nil {
		displayPath = filepath.Join(dir, DerivedDir, "display.jpg")
	}

	years, _ := age.At(artist.BirthDate.Time, artworkDate.Time)
	cleanup = false
	return &ArtworkRecord{
		ArtistSlug:   artist.Slug,
		ArtistName:   artist.Name,
		Folder:       folder,
		Dir:          dir,
		OriginalName: originalName,
		OriginalPath: originalPath,
		DisplayPath:  displayPath,
		ThumbPath:    thumbPath,
		Meta:         meta,
		AgeYears:     years,
	}, nil
}

// findByUploadID returns the artist's artwork with this id, or nil.
// Callers hold the artist lock; the scan is the whole check-then-act.
func (s *Store) findByUploadID(artist Artist, uploadID string) (*ArtworkRecord, error) {
	dir := filepath.Join(s.ArtistsRoot(), artist.Slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rec, err := s.loadArtworkDir(artist, e.Name())
		if err != nil {
			continue
		}
		if rec.Meta.UploadID == uploadID {
			return rec, nil
		}
	}
	return nil, nil
}

// WalkArtworks visits every artwork on disk (source of truth for reindex).
func (s *Store) WalkArtworks() ([]ArtworkRecord, error) {
	artists, err := s.ListArtists()
	if err != nil {
		return nil, err
	}
	var out []ArtworkRecord
	for _, artist := range artists {
		recs, err := s.listArtistArtworks(artist)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Meta.Date.Equal(out[j].Meta.Date.Time) {
			return out[i].Meta.Date.After(out[j].Meta.Date.Time)
		}
		return out[i].Folder < out[j].Folder
	})
	return out, nil
}

func (s *Store) listArtistArtworks(artist Artist) ([]ArtworkRecord, error) {
	dir := filepath.Join(s.ArtistsRoot(), artist.Slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []ArtworkRecord
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rec, err := s.loadArtworkDir(artist, e.Name())
		if err != nil {
			continue
		}
		out = append(out, *rec)
	}
	return out, nil
}

func (s *Store) LoadArtwork(artistSlug, folder string) (*ArtworkRecord, error) {
	artist, err := s.LoadArtist(artistSlug)
	if err != nil {
		return nil, err
	}
	return s.loadArtworkDir(*artist, folder)
}

func (s *Store) loadArtworkDir(artist Artist, folder string) (*ArtworkRecord, error) {
	dir := filepath.Join(s.ArtistsRoot(), artist.Slug, folder)
	var meta Artwork
	if err := readJSON(filepath.Join(dir, ArtworkFile), &meta); err != nil {
		return nil, err
	}
	if meta.Crop != nil {
		if err := meta.Crop.Validate(); err != nil {
			return nil, err
		}
	}
	originalName, originalPath, err := findOriginal(dir)
	if err != nil {
		return nil, err
	}
	years, err := age.At(artist.BirthDate.Time, meta.Date.Time)
	if err != nil {
		return nil, err
	}
	thumbPath := filepath.Join(dir, DerivedDir, ThumbName)
	if _, err := os.Stat(thumbPath); err != nil {
		thumbPath = ""
	}
	displayPath := filepath.Join(dir, DerivedDir, "display.jpg")
	if _, err := os.Stat(displayPath); err != nil {
		displayPath = ""
	}
	return &ArtworkRecord{
		ArtistSlug:   artist.Slug,
		ArtistName:   artist.Name,
		Folder:       folder,
		Dir:          dir,
		OriginalName: originalName,
		OriginalPath: originalPath,
		DisplayPath:  displayPath,
		ThumbPath:    thumbPath,
		Meta:         meta,
		AgeYears:     years,
	}, nil
}

func findOriginal(dir string) (name, path string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, "original.") {
			return n, filepath.Join(dir, n), nil
		}
	}
	return "", "", fmt.Errorf("no original.* in %s", dir)
}

func cleanTags(tags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// RegenerateDerived reads only the archive original and artwork.json.
func (s *Store) RegenerateDerived(artist, folder string) error {
	if err := safeSlug(artist); err != nil {
		return err
	}
	if err := safeSlug(folder); err != nil {
		return err
	}
	dir := filepath.Join(s.ArtistsRoot(), artist, folder)
	var meta Artwork
	if err := readJSON(filepath.Join(dir, ArtworkFile), &meta); err != nil {
		return err
	}
	_, originalPath, err := findOriginal(dir)
	if err != nil {
		return err
	}
	return s.regenerateDerived(originalPath, meta)
}

func (s *Store) regenerateDerived(originalPath string, meta Artwork) error {
	dir := filepath.Join(filepath.Dir(originalPath), DerivedDir)
	var orientation int
	var err error
	if meta.Crop != nil {
		orientation, err = imageutil.WriteCropped(originalPath, dir, *meta.Crop)
	} else {
		orientation, err = imageutil.WriteThumbnail(originalPath, filepath.Join(dir, ThumbName), 400)
	}
	if err != nil {
		return err
	}
	thumbMeta := map[string]any{"schema_version": SchemaVersion, "kind": "thumbnail", "source": filepath.Base(originalPath), "max_edge": 400, "format": "jpeg", "orientation": orientation}
	if meta.Crop != nil {
		thumbMeta["crop"] = meta.Crop
	}
	return writeJSON(filepath.Join(dir, ThumbMetaName), thumbMeta)
}
