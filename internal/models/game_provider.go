package models

import "time"

// SourceIGDB tags records cached from the IGDB API.
const SourceIGDB = "igdb"

// GameSearchResult is one game returned by a provider's search. It carries only
// what a result card renders — full details are fetched separately.
type GameSearchResult struct {
	ExternalID  int
	Name        string
	CoverURL    string
	ReleaseYear int
}

// GameDetails is the enriched record fetched the first time a game is selected.
// Both types live here, rather than inside a provider package, so the layers
// above the client never name the provider they happen to be fed by.
type GameDetails struct {
	ExternalID  int
	Name        string
	CoverURL    string
	ReleasedAt  *time.Time
	Developer   string
	Description string
}
