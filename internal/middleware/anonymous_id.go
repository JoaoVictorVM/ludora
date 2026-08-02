package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// CookieName is the cookie carrying the anonymous reviewer identifier.
const CookieName = "ludora_uid"

// cookieMaxAge is roughly two years, the lifetime an anonymous identity keeps
// across visits before the visitor is treated as someone new.
const cookieMaxAge = 2 * 365 * 24 * time.Hour

type contextKey struct{}

var reviewerIDKey contextKey

// AnonymousID guarantees every request carries a reviewer identifier: it reuses
// a valid ludora_uid cookie when present and otherwise mints a new UUIDv4,
// setting it on the response. The identifier is exposed to downstream handlers
// through the request context.
func AnonymousID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reviewerID, ok := existingReviewerID(r)
		if !ok {
			reviewerID = uuid.NewString()
			http.SetCookie(w, newCookie(reviewerID))
		}

		ctx := context.WithValue(r.Context(), reviewerIDKey, reviewerID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ReviewerID returns the anonymous identifier injected by AnonymousID.
func ReviewerID(ctx context.Context) (string, bool) {
	reviewerID, ok := ctx.Value(reviewerIDKey).(string)
	return reviewerID, ok && reviewerID != ""
}

func existingReviewerID(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return "", false
	}

	// A tampered or truncated value is discarded rather than trusted, so the
	// identifier downstream is always a well-formed UUID.
	parsed, err := uuid.Parse(cookie.Value)
	if err != nil {
		return "", false
	}

	return parsed.String(), true
}

func newCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Expires:  time.Now().Add(cookieMaxAge),
		MaxAge:   int(cookieMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}
