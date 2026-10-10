package store

import (
	"fmt"
	"strings"
	"time"
)

// Date is a calendar day serialized as YYYY-MM-DD in JSON.
type Date struct {
	time.Time
}

func NewDate(t time.Time) Date {
	return Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)}
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q: %w", s, err)
	}
	return NewDate(t), nil
}

func (d Date) MarshalJSON() ([]byte, error) {
	if d.Time.IsZero() {
		return []byte(`""`), nil
	}
	return []byte(`"` + d.Time.Format("2006-01-02") + `"`), nil
}

func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		d.Time = time.Time{}
		return nil
	}
	// Accept full RFC3339 too, for robustness when reading older scrapes.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		*d = NewDate(t)
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("invalid date %q", s)
	}
	*d = NewDate(t)
	return nil
}

// Artist is persisted as artists/<slug>/artist.json.
type Artist struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	BirthDate     Date   `json:"birth_date"`
	Slug          string `json:"slug"`
}

// Artwork is persisted as artwork.json beside original.* and derived/.
type Artwork struct {
	SchemaVersion int      `json:"schema_version"`
	Title         string   `json:"title"`
	Date          Date     `json:"date"`
	Description   string   `json:"description,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	GiftedTo      string   `json:"gifted_to,omitempty"`
}

// ArtworkRecord is an artwork plus resolved paths and artist context.
type ArtworkRecord struct {
	ArtistSlug   string
	ArtistName   string
	Folder       string
	Dir          string
	OriginalName string
	OriginalPath string
	ThumbPath    string
	Meta         Artwork
	AgeYears     int
}

const (
	SchemaVersion   = 1
	ArtistsDir      = "artists"
	MembersDir      = "members"
	InvitesDir      = "invites"
	DerivedDir      = "derived"
	ThumbName       = "thumb.jpg"
	ThumbMetaName   = "thumb.json"
	ArtistFile      = "artist.json"
	ArtworkFile     = "artwork.json"
	MemberFile      = "member.json"
	CredentialsFile = "credentials.json"
	IndexDBName     = "index.db"
	// InviteTTL is how long a one-time invite link stays redeemable.
	InviteTTL = 7 * 24 * time.Hour
)

// Member is persisted as members/<id>/member.json. ID is 32 lowercase hex
// characters (16 random bytes), also the directory name.
type Member struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	CreatedAt     time.Time `json:"created_at"`
}

// Passkey is one public credential in credentials.json. The id, public key,
// counter and date are the credential record; the remaining fields are what
// WebAuthn needs to accept the same credential after the index is rebuilt.
type Passkey struct {
	ID              string    `json:"id"`
	PublicKey       string    `json:"public_key"`
	Counter         uint32    `json:"counter"`
	CreatedAt       time.Time `json:"created_at"`
	AAGUID          string    `json:"aaguid,omitempty"`
	BackupEligible  bool      `json:"backup_eligible"`
	BackupState     bool      `json:"backup_state"`
	UserPresent     bool      `json:"user_present"`
	UserVerified    bool      `json:"user_verified"`
	AttestationType string    `json:"attestation_type,omitempty"`
}

// PasskeysFile is members/<id>/credentials.json.
type PasskeysFile struct {
	SchemaVersion int       `json:"schema_version"`
	Credentials   []Passkey `json:"credentials"`
}

// Invite is invites/<sha256(token)>.json. The token itself is never stored.
type Invite struct {
	SchemaVersion int        `json:"schema_version"`
	Name          string     `json:"name"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	UsedAt        *time.Time `json:"used_at,omitempty"`
}

// MemberRecord is a member plus the passkeys stored beside them.
type MemberRecord struct {
	Member   Member
	Passkeys []Passkey
}
