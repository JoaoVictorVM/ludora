package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// captureReviewerID returns a handler that records the identifier the middleware
// placed in the request context.
func captureReviewerID(seen *string, found *bool) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		*seen, *found = ReviewerID(r.Context())
	})
}

func TestMiddleware_SetsCookieOnFirstRequest(t *testing.T) {
	var seen string
	var found bool

	rec := httptest.NewRecorder()
	AnonymousID(captureReviewerID(&seen, &found)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	cookie := findCookie(rec.Result().Cookies(), CookieName)
	if cookie == nil {
		t.Fatalf("expected a %s cookie to be set", CookieName)
	}

	parsed, err := uuid.Parse(cookie.Value)
	if err != nil {
		t.Fatalf("cookie value %q is not a valid UUID: %v", cookie.Value, err)
	}
	if parsed.Version() != 4 {
		t.Errorf("cookie UUID version = %d, want 4", parsed.Version())
	}

	if !found {
		t.Fatal("expected the reviewer id to be present in the request context")
	}
	if seen != cookie.Value {
		t.Errorf("context id = %q, cookie = %q; want the same value", seen, cookie.Value)
	}
}

func TestMiddleware_ReusesExistingCookie(t *testing.T) {
	var seen string
	var found bool

	existing := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: existing})

	rec := httptest.NewRecorder()
	AnonymousID(captureReviewerID(&seen, &found)).ServeHTTP(rec, req)

	if cookie := findCookie(rec.Result().Cookies(), CookieName); cookie != nil {
		t.Errorf("expected no Set-Cookie on a request that already had one, got %q", cookie.Value)
	}
	if !found {
		t.Fatal("expected the reviewer id to be present in the request context")
	}
	if seen != existing {
		t.Errorf("context id = %q, want the existing cookie value %q", seen, existing)
	}
}

func TestMiddleware_RegeneratesInvalidCookie(t *testing.T) {
	var seen string
	var found bool

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "not-a-uuid"})

	rec := httptest.NewRecorder()
	AnonymousID(captureReviewerID(&seen, &found)).ServeHTTP(rec, req)

	cookie := findCookie(rec.Result().Cookies(), CookieName)
	if cookie == nil {
		t.Fatal("expected a replacement cookie for a malformed value")
	}
	if _, err := uuid.Parse(cookie.Value); err != nil {
		t.Fatalf("replacement cookie %q is not a valid UUID: %v", cookie.Value, err)
	}
	if !found || seen != cookie.Value {
		t.Errorf("context id = %q (found=%t), want the replacement cookie value", seen, found)
	}
}

func TestMiddleware_CookieFlags(t *testing.T) {
	rec := httptest.NewRecorder()
	handler := AnonymousID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	cookie := findCookie(rec.Result().Cookies(), CookieName)
	if cookie == nil {
		t.Fatalf("expected a %s cookie to be set", CookieName)
	}

	if !cookie.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("cookie Path = %q, want /", cookie.Path)
	}

	wantExpiry := time.Now().Add(cookieMaxAge)
	if diff := cookie.Expires.Sub(wantExpiry); diff > time.Hour || diff < -time.Hour {
		t.Errorf("cookie expires at %v, want approximately %v", cookie.Expires, wantExpiry)
	}
	if cookie.MaxAge != int(cookieMaxAge.Seconds()) {
		t.Errorf("cookie MaxAge = %d, want %d", cookie.MaxAge, int(cookieMaxAge.Seconds()))
	}
}
