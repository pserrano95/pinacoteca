package store

import (
	"fmt"
	"path/filepath"
)

// SetArtworkHidden updates only metadata, authorized by the artist on disk.
func (s *Store) SetArtworkHidden(artistSlug, folder, memberID string, hidden bool) error {
	if err := safeSlug(artistSlug); err != nil {
		return err
	}
	if err := safeSlug(folder); err != nil {
		return err
	}
	path := filepath.Join(s.ArtistsRoot(), artistSlug, folder, ArtworkFile)
	unlock := lockGallery(path)
	defer unlock()
	artist, err := s.LoadArtist(artistSlug)
	if err != nil {
		return err
	}
	if !artist.IsGuardian(memberID) {
		return fmt.Errorf("guardian required")
	}
	rec, err := s.LoadArtwork(artistSlug, folder)
	if err != nil {
		return err
	}
	rec.Meta.Hidden = hidden
	return writeJSON(path, &rec.Meta)
}
