package models

import "time"

// SourceRAWG tags records cached from the RAWG API, leaving room for other
// providers without a schema change.
const SourceRAWG = "rawg"

// Game is a locally cached game record. Nullable text columns are exposed as
// empty strings so views can render them without pointer checks; ReleasedAt
// stays a pointer because "no known release date" is meaningful.
type Game struct {
	ID             int64
	ExternalID     string
	ExternalSource string
	Name           string
	CoverURL       string
	ReleasedAt     *time.Time
	Developer      string
	Description    string
	CreatedAt      time.Time
}

// ReleaseYear returns 0 when the release date is unknown.
func (g *Game) ReleaseYear() int {
	if g.ReleasedAt == nil {
		return 0
	}
	return g.ReleasedAt.Year()
}
