package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

func (s *Store) EnsureLayout() error {
	return os.MkdirAll(s.ArtistsRoot(), 0o755)
}

// CreateArtist writes artists/<slug>/artist.json. Slug collisions get a numeric suffix.
func (s *Store) CreateArtist(name string, birthDate time.Time) (*Artist, error) {
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
	path := filepath.Join(s.ArtistsRoot(), artistSlug, ArtistFile)
	var a Artist
	if err := readJSON(path, &a); err != nil {
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
		a, err := s.LoadArtist(e.Name())
		if err != nil {
			continue
		}
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateArtworkInput is the upload payload (original bytes unchanged on disk).
type CreateArtworkInput struct {
	ArtistSlug  string
	Title       string
	Date        time.Time
	Description string
	Tags        []string
	GiftedTo    string
	ImageBytes  []byte
}

// CreateArtwork validates, writes original + artwork.json + derived thumb, and returns the record.
// On any failure after creating the folder, the folder is removed so nothing is left on disk.
func (s *Store) CreateArtwork(in CreateArtworkInput) (*ArtworkRecord, error) {
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
		SchemaVersion: SchemaVersion,
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
	if err := imageutil.WriteThumbnail(originalPath, thumbPath, 400); err != nil {
		return nil, fmt.Errorf("thumbnail: %w", err)
	}
	thumbMeta := map[string]any{
		"schema_version": SchemaVersion,
		"kind":           "thumbnail",
		"source":         originalName,
		"max_edge":       400,
		"format":         "jpeg",
	}
	if err := writeJSON(filepath.Join(dir, DerivedDir, ThumbMetaName), thumbMeta); err != nil {
		return nil, err
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
		ThumbPath:    thumbPath,
		Meta:         meta,
		AgeYears:     years,
	}, nil
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
	return &ArtworkRecord{
		ArtistSlug:   artist.Slug,
		ArtistName:   artist.Name,
		Folder:       folder,
		Dir:          dir,
		OriginalName: originalName,
		OriginalPath: originalPath,
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
	return os.WriteFile(path, data, 0o644)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
