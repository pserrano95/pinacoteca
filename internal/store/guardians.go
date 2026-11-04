package store

import (
	"fmt"
	"path/filepath"
	"slices"
	"time"
)

func validateArtistPermissions(a Artist) error {
	seen := map[string]bool{}
	for _, id := range a.Guardians {
		if !validMemberID(id) || seen[id] {
			return fmt.Errorf("invalid or duplicate guardian")
		}
		seen[id] = true
	}
	seen = map[string]bool{}
	for _, slug := range a.ApprovedGalleries {
		if err := safeSlug(slug); err != nil || seen[slug] {
			return fmt.Errorf("invalid or duplicate approved gallery")
		}
		seen[slug] = true
	}
	return nil
}

func (a Artist) IsGuardian(memberID string) bool { return slices.Contains(a.Guardians, memberID) }

// CreateArtistWithGuardian writes the provisional guardian in the initial file.
func (s *Store) CreateArtistWithGuardian(name string, birth time.Time, memberID string) (*Artist, error) {
	if _, err := s.LoadMemberRecord(memberID); err != nil {
		return nil, err
	}
	return s.createArtist(name, birth, []string{memberID})
}

// AddArtistGuardian is a host operation; existing approvals remain explicit.
func (s *Store) AddArtistGuardian(artistSlug, memberID string) error {
	if _, err := s.LoadMemberRecord(memberID); err != nil {
		return err
	}
	return s.updateArtistPermissions(artistSlug, func(a *Artist) error {
		if !a.IsGuardian(memberID) {
			a.Guardians = append(a.Guardians, memberID)
		}
		return nil
	})
}

// ApproveArtistGallery authorizes from the archive, never from a gallery role.
func (s *Store) ApproveArtistGallery(artistSlug, gallerySlug, memberID string) error {
	g, err := s.LoadGallery(gallerySlug)
	if err != nil {
		return err
	}
	if !slices.Contains(g.Artists, artistSlug) {
		return fmt.Errorf("artist is not requested by gallery")
	}
	return s.updateArtistPermissions(artistSlug, func(a *Artist) error {
		if !a.IsGuardian(memberID) {
			return fmt.Errorf("guardian required")
		}
		if !slices.Contains(a.ApprovedGalleries, gallerySlug) {
			a.ApprovedGalleries = append(a.ApprovedGalleries, gallerySlug)
		}
		return nil
	})
}

func (s *Store) updateArtistPermissions(artistSlug string, update func(*Artist) error) error {
	if err := safeSlug(artistSlug); err != nil {
		return err
	}
	path := filepath.Join(s.ArtistsRoot(), artistSlug, ArtistFile)
	// The shared path lock serializes read/modify/write across Store instances.
	unlock := lockGallery(path)
	defer unlock()
	a, err := s.LoadArtist(artistSlug)
	if err != nil {
		return err
	}
	if err := update(a); err != nil {
		return err
	}
	if err := validateArtistPermissions(*a); err != nil {
		return err
	}
	return writeJSON(path, a)
}
