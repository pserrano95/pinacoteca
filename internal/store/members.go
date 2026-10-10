package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CreateMember writes a new member directory with its passkeys.
func (s *Store) CreateMember(name string, creds []Passkey, now time.Time) (*Member, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	m := Member{
		SchemaVersion: SchemaVersion,
		ID:            hex.EncodeToString(raw),
		Name:          strings.TrimSpace(name),
		CreatedAt:     now.UTC().Truncate(time.Second),
	}
	if err := s.SaveMember(m, creds); err != nil {
		return nil, err
	}
	return &m, nil
}

// SaveMember writes members/<id>/member.json and credentials.json.
// On failure the directory is removed.
func (s *Store) SaveMember(m Member, creds []Passkey) error {
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		return fmt.Errorf("member name is required")
	}
	if !validMemberID(m.ID) {
		return fmt.Errorf("invalid member id")
	}
	if len(creds) == 0 {
		return fmt.Errorf("member requires a passkey")
	}
	m.SchemaVersion = SchemaVersion
	m.CreatedAt = m.CreatedAt.UTC().Truncate(time.Second)
	for i := range creds {
		if creds[i].ID == "" || creds[i].PublicKey == "" {
			return fmt.Errorf("passkey id and public key are required")
		}
		creds[i].CreatedAt = creds[i].CreatedAt.UTC().Truncate(time.Second)
	}
	dir := filepath.Join(s.MembersRoot(), m.ID)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("member %s already exists", m.ID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
		}
	}()
	if err := writeJSON(filepath.Join(dir, MemberFile), &m); err != nil {
		return err
	}
	file := PasskeysFile{SchemaVersion: SchemaVersion, Credentials: creds}
	if err := writeJSON(filepath.Join(dir, CredentialsFile), &file); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// ListMemberRecords reads every member directory. A directory without a
// readable member.json is skipped.
func (s *Store) ListMemberRecords() ([]MemberRecord, error) {
	entries, err := os.ReadDir(s.MembersRoot())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []MemberRecord
	for _, e := range entries {
		if !e.IsDir() || !validMemberID(e.Name()) {
			continue
		}
		rec, err := s.LoadMemberRecord(e.Name())
		if err != nil {
			continue
		}
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Member.ID < out[j].Member.ID })
	return out, nil
}

// LoadMemberRecord reads one member and their passkeys.
func (s *Store) LoadMemberRecord(id string) (*MemberRecord, error) {
	if !validMemberID(id) {
		return nil, fmt.Errorf("invalid member id")
	}
	dir := filepath.Join(s.MembersRoot(), id)
	var m Member
	if err := readJSON(filepath.Join(dir, MemberFile), &m); err != nil {
		return nil, err
	}
	m.ID = id
	var file PasskeysFile
	if err := readJSON(filepath.Join(dir, CredentialsFile), &file); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &MemberRecord{Member: m}, nil
		}
		return nil, err
	}
	sort.Slice(file.Credentials, func(i, j int) bool { return file.Credentials[i].ID < file.Credentials[j].ID })
	return &MemberRecord{Member: m, Passkeys: file.Credentials}, nil
}

// UpdatePasskey writes the counter and flags of one credential back to disk
// so a later reindex does not roll the sign count backwards.
func (s *Store) UpdatePasskey(memberID string, pk Passkey) error {
	rec, err := s.LoadMemberRecord(memberID)
	if err != nil {
		return err
	}
	found := false
	for i := range rec.Passkeys {
		if rec.Passkeys[i].ID == pk.ID {
			rec.Passkeys[i].Counter = pk.Counter
			rec.Passkeys[i].BackupEligible = pk.BackupEligible
			rec.Passkeys[i].BackupState = pk.BackupState
			rec.Passkeys[i].UserPresent = pk.UserPresent
			rec.Passkeys[i].UserVerified = pk.UserVerified
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("passkey %s not found", pk.ID)
	}
	file := PasskeysFile{SchemaVersion: SchemaVersion, Credentials: rec.Passkeys}
	return writeJSON(filepath.Join(s.MembersRoot(), memberID, CredentialsFile), &file)
}

func validMemberID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}
