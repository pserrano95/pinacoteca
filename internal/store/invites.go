package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrInviteRejected means the token is missing, already used, or expired.
// Callers must not tell those cases apart.
var ErrInviteRejected = errors.New("invite rejected")

// CreateInvite writes the hash of a new one-time token and returns the link.
// The token is not stored. now is the clock, so tests can pin it.
// The invite has no gallery: it is the bootstrap link the CLI prints.
func (s *Store) CreateInvite(name, baseURL string, now time.Time) (string, error) {
	return s.writeInvite(name, "", "", baseURL, now)
}

// CreateGalleryInvite is a one-time link into one gallery as contributor or
// viewer. The role is written to gallery.json when the link is redeemed.
func (s *Store) CreateGalleryInvite(name, gallerySlug, role, baseURL string, now time.Time) (string, error) {
	if role != RoleContributor && role != RoleViewer {
		return "", fmt.Errorf("invite role must be contributor or viewer")
	}
	if _, err := s.LoadGallery(gallerySlug); err != nil {
		return "", fmt.Errorf("gallery: %w", err)
	}
	return s.writeInvite(name, gallerySlug, role, baseURL, now)
}

func (s *Store) writeInvite(name, gallerySlug, role, baseURL string, now time.Time) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("invite name is required")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid base url")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now = now.UTC().Truncate(time.Second)
	inv := Invite{
		SchemaVersion: SchemaVersion,
		Name:          name,
		GallerySlug:   gallerySlug,
		Role:          role,
		CreatedAt:     now,
		ExpiresAt:     now.Add(InviteTTL),
	}
	if err := os.MkdirAll(s.InvitesRoot(), 0o755); err != nil {
		return "", err
	}
	if err := writeJSON(s.invitePath(token), &inv); err != nil {
		return "", err
	}
	return baseURL + "/invite/" + token, nil
}

// RedeemInvite saves the new member, writes their gallery role when the
// invite names one, and marks the token used. A failure leaves neither the
// member nor a half-applied role behind, and the token stays redeemable.
func (s *Store) RedeemInvite(token string, member Member, passkeys []Passkey, now time.Time) error {
	inv, err := s.LookupInvite(token, now)
	if err != nil {
		return err
	}
	if inv.GallerySlug != "" && inv.Role != RoleContributor && inv.Role != RoleViewer {
		return ErrInviteRejected
	}
	if err := s.SaveMember(member, passkeys); err != nil {
		return err
	}
	rollback := func() {
		if inv.GallerySlug != "" {
			_ = s.removeGalleryMember(inv.GallerySlug, member.ID)
		}
		_ = os.RemoveAll(filepath.Join(s.MembersRoot(), member.ID))
	}
	if inv.GallerySlug != "" {
		if err := s.AddGalleryMember(inv.GallerySlug, member.ID, inv.Role); err != nil {
			rollback()
			return err
		}
	}
	if err := s.MarkInviteUsed(token, now); err != nil {
		rollback()
		return err
	}
	return nil
}

// LookupInvite returns the invite when the token is still redeemable.
func (s *Store) LookupInvite(token string, now time.Time) (*Invite, error) {
	inv, err := s.readInvite(token)
	if err != nil {
		return nil, ErrInviteRejected
	}
	if inv.UsedAt != nil || !now.Before(inv.ExpiresAt) {
		return nil, ErrInviteRejected
	}
	return inv, nil
}

// MarkInviteUsed records a successful redemption. A second call is rejected.
func (s *Store) MarkInviteUsed(token string, now time.Time) error {
	inv, err := s.readInvite(token)
	if err != nil {
		return ErrInviteRejected
	}
	if inv.UsedAt != nil || !now.Before(inv.ExpiresAt) {
		return ErrInviteRejected
	}
	t := now.UTC().Truncate(time.Second)
	inv.UsedAt = &t
	return writeJSON(s.invitePath(token), inv)
}

func (s *Store) readInvite(token string) (*Invite, error) {
	if token == "" || strings.Contains(token, "/") {
		return nil, ErrInviteRejected
	}
	var inv Invite
	if err := readJSON(s.invitePath(token), &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

func (s *Store) invitePath(token string) string {
	return filepath.Join(s.InvitesRoot(), InviteHash(token)+".json")
}

// InviteHash is the hex SHA-256 of the token string. It is the file name.
func InviteHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
