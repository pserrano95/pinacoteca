package age

import (
	"fmt"
	"time"
)

// At returns the artist's age in whole years on artworkDate.
// Both dates are calendar dates in local civil time (YYYY-MM-DD semantics).
func At(birthDate, artworkDate time.Time) (int, error) {
	b := dateOnly(birthDate)
	a := dateOnly(artworkDate)
	if a.Before(b) {
		return 0, fmt.Errorf("artwork date %s is before birth date %s", a.Format("2006-01-02"), b.Format("2006-01-02"))
	}
	years := a.Year() - b.Year()
	anniversary := time.Date(a.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	if a.Before(anniversary) {
		years--
	}
	return years, nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
