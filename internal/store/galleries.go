package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/pserrano95/pinacoteca/internal/slug"
)

var (
	galleryLocksMu sync.Mutex
	galleryLocks   = map[string]*sync.Mutex{}
)

func lockGallery(path string) func() {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	} else {
		path = filepath.Clean(path)
	}
	galleryLocksMu.Lock()
	mu, ok := galleryLocks[path]
	if !ok {
		mu = &sync.Mutex{}
		galleryLocks[path] = mu
	}
	galleryLocksMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func (s *Store) galleryPath(gallerySlug string) string {
	return filepath.Join(s.GalleriesRoot(), gallerySlug, GalleryFile)
}

// CreateGallery writes galleries/<slug>/gallery.json. An empty wall colour
// becomes the off-white default. Slug collisions get a numeric suffix.
func (s *Store) CreateGallery(name, wallColour string) (*Gallery, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("gallery name is required")
	}
	wallColour = strings.TrimSpace(wallColour)
	if wallColour == "" {
		wallColour = WallColourDefault
	}
	if !ValidWallColour(wallColour) {
		return nil, fmt.Errorf("unknown wall colour %q", wallColour)
	}
	if err := os.MkdirAll(s.GalleriesRoot(), 0o755); err != nil {
		return nil, err
	}
	base := slug.Make(name)
	gallerySlug := base
	var dir string
	for i := 2; ; i++ {
		dir = filepath.Join(s.GalleriesRoot(), gallerySlug)
		if err := os.Mkdir(dir, 0o755); err == nil {
			break
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		gallerySlug = fmt.Sprintf("%s-%d", base, i)
	}
	unlock := lockGallery(s.galleryPath(gallerySlug))
	defer unlock()

	g := &Gallery{
		SchemaVersion: SchemaVersion,
		Name:          name,
		Slug:          gallerySlug,
		WallColour:    wallColour,
		Artists:       []string{},
		Members:       []GalleryMember{},
	}
	if err := writeJSON(s.galleryPath(gallerySlug), g); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return g, nil
}

// AddGalleryArtist appends an existing artist slug. The artist folder is not copied.
func (s *Store) AddGalleryArtist(gallerySlug, artistSlug string) error {
	if err := safeSlug(gallerySlug); err != nil {
		return err
	}
	if err := safeSlug(artistSlug); err != nil {
		return err
	}
	if _, err := s.LoadArtist(artistSlug); err != nil {
		return fmt.Errorf("artist: %w", err)
	}
	unlock := lockGallery(s.galleryPath(gallerySlug))
	defer unlock()

	g, err := s.LoadGallery(gallerySlug)
	if err != nil {
		return err
	}
	for _, existing := range g.Artists {
		if existing == artistSlug {
			return fmt.Errorf("artist %s already in gallery %s", artistSlug, gallerySlug)
		}
	}
	g.Artists = append(g.Artists, artistSlug)
	return writeJSON(s.galleryPath(gallerySlug), g)
}

// AddGalleryMember appends a member who already exists on disk, with a gallery role.
func (s *Store) AddGalleryMember(gallerySlug, memberID, role string) error {
	if err := safeSlug(gallerySlug); err != nil {
		return err
	}
	if !validMemberID(memberID) {
		return fmt.Errorf("invalid member id")
	}
	if !ValidRole(role) {
		return fmt.Errorf("role must be owner, contributor or viewer")
	}
	if _, err := s.LoadMemberRecord(memberID); err != nil {
		return fmt.Errorf("member: %w", err)
	}
	unlock := lockGallery(s.galleryPath(gallerySlug))
	defer unlock()

	g, err := s.LoadGallery(gallerySlug)
	if err != nil {
		return err
	}
	for _, m := range g.Members {
		if m.ID == memberID {
			return fmt.Errorf("member %s already in gallery %s", memberID, gallerySlug)
		}
	}
	g.Members = append(g.Members, GalleryMember{ID: memberID, Role: role})
	return writeJSON(s.galleryPath(gallerySlug), g)
}

// removeGalleryMember drops one member row. RedeemInvite uses it when a
// redemption cannot be finished, so gallery.json does not keep a role for a
// member that was never created.
func (s *Store) removeGalleryMember(gallerySlug, memberID string) error {
	if err := safeSlug(gallerySlug); err != nil {
		return err
	}
	unlock := lockGallery(s.galleryPath(gallerySlug))
	defer unlock()

	g, err := s.LoadGallery(gallerySlug)
	if err != nil {
		return err
	}
	next := make([]GalleryMember, 0, len(g.Members))
	for _, m := range g.Members {
		if m.ID != memberID {
			next = append(next, m)
		}
	}
	g.Members = next
	return writeJSON(s.galleryPath(gallerySlug), g)
}

// LoadGallery reads one gallery. The directory name is the slug; a mismatch is an error.
func (s *Store) LoadGallery(gallerySlug string) (*Gallery, error) {
	if err := safeSlug(gallerySlug); err != nil {
		return nil, err
	}
	var g Gallery
	if err := readJSON(s.galleryPath(gallerySlug), &g); err != nil {
		return nil, err
	}
	if g.Slug != gallerySlug {
		return nil, fmt.Errorf("gallery slug %q does not match directory %q", g.Slug, gallerySlug)
	}
	if !ValidWallColour(g.WallColour) {
		return nil, fmt.Errorf("unknown wall colour %q", g.WallColour)
	}
	if g.Artists == nil {
		g.Artists = []string{}
	}
	if g.Members == nil {
		g.Members = []GalleryMember{}
	}
	seenArtist := map[string]bool{}
	for _, artistSlug := range g.Artists {
		if err := safeSlug(artistSlug); err != nil || seenArtist[artistSlug] {
			return nil, fmt.Errorf("invalid artists in gallery %s", gallerySlug)
		}
		seenArtist[artistSlug] = true
	}
	seenMember := map[string]bool{}
	for _, m := range g.Members {
		if !validMemberID(m.ID) || !ValidRole(m.Role) || seenMember[m.ID] {
			return nil, fmt.Errorf("invalid member in gallery %s", gallerySlug)
		}
		seenMember[m.ID] = true
	}
	return &g, nil
}

// ListGalleries reads every gallery directory. A directory without gallery.json
// is skipped. A gallery.json that does not load fails the call, so a reindex
// does not drop permissions in silence.
func (s *Store) ListGalleries() ([]Gallery, error) {
	entries, err := os.ReadDir(s.GalleriesRoot())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Gallery
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := s.galleryPath(e.Name())
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		g, err := s.LoadGallery(e.Name())
		if err != nil {
			return nil, fmt.Errorf("gallery %s: %w", e.Name(), err)
		}
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func safeSlug(s string) error {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) || filepath.Base(s) != s {
		return fmt.Errorf("invalid slug %q", s)
	}
	return nil
}
