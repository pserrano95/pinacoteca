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
	SchemaVersion = 1
	ArtistsDir    = "artists"
	DerivedDir    = "derived"
	ThumbName     = "thumb.jpg"
	ThumbMetaName = "thumb.json"
	ArtistFile    = "artist.json"
	ArtworkFile   = "artwork.json"
	IndexDBName   = "index.db"
)
