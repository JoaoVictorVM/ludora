package models

import "time"

// Rating bounds in half-star units: 1 is half a star, 10 is five stars.
const (
	MinRating = 1
	MaxRating = 10

	MaxCommentLength = 800
)

// Messages shown when a submission is rejected. They live here because both the
// handler and the form template need to agree on the duplicate case, which is
// the only one that also renders a link to the existing review.
const (
	DuplicateReviewMessage = "Você já avaliou este jogo. Edite sua review existente."
	MissingRatingMessage   = "Selecione uma nota antes de publicar."
	CommentTooLongMessage  = "Sua review é muito longa (máximo de 800 caracteres)."

	// NotAuthorizedMessage is deliberately vague: it is shown both when the
	// review belongs to someone else and when it does not exist at all.
	NotAuthorizedMessage  = "Você não tem permissão para modificar esta review."
	AlreadyRemovedMessage = "Esta review já foi removida."
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
