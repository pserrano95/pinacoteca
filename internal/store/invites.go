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
func (s *Store) CreateInvite(name, baseURL string, now time.Time) (string, error) {
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
