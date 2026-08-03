package models

import "time"

// Rating bounds in half-star units: 1 is half a star, 10 is five stars.
const (
	MinRating = 1
	MaxRating = 10

	MaxCommentLength = 800
)

// Review is a single anonymous review of a game.
type Review struct {
	ID           int64
	GameID       int64
	ReviewerUUID string
	Rating       int16
	Comment      string
	CreatedAt    time.Time
}

// Stars converts the half-star rating into its 0.5–5.0 representation.
func (r *Review) Stars() float64 {
	return float64(r.Rating) / 2
}
