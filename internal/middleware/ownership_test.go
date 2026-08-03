package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubOwner struct {
	owner string
	err   error
	calls int
}

func (s *stubOwner) OwnerOf(context.Context, int64) (string, error) {
	s.calls++
	return s.owner, s.err
}

// contextWithReviewer runs a request through AnonymousID to obtain a context
// carrying the given identifier, the same way the real handlers get it.
func contextWithReviewer(t *testing.T, reviewerUUID string) context.Context {
	t.Helper()

	var ctx context.Context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if reviewerUUID != "" {
		req.AddCookie(&http.Cookie{Name: CookieName, Value: reviewerUUID})
	}

	AnonymousID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), req)

	return ctx
}

func TestRequireOwnership_AllowsTheOwner(t *testing.T) {
	const owner = "11111111-1111-4111-8111-111111111111"

	err := RequireOwnership(contextWithReviewer(t, owner), &stubOwner{owner: owner}, 1)
	if err != nil {
		t.Fatalf("RequireOwnership = %v, want nil for the owner", err)
	}
}

func TestRequireOwnership_RejectsAnotherVisitor(t *testing.T) {
	ctx := contextWithReviewer(t, "22222222-2222-4222-8222-222222222222")

	err := RequireOwnership(ctx, &stubOwner{owner: "11111111-1111-4111-8111-111111111111"}, 1)
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("RequireOwnership = %v, want ErrNotAuthorized", err)
	}
}

// A review that does not exist must be indistinguishable from one owned by
// somebody else, so ids cannot be probed.
func TestRequireOwnership_MissingResourceLooksLikeNotAuthorized(t *testing.T) {
	ctx := contextWithReviewer(t, "11111111-1111-4111-8111-111111111111")

	err := RequireOwnership(ctx, &stubOwner{owner: ""}, 999)
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("RequireOwnership = %v, want ErrNotAuthorized", err)
	}
}

func TestRequireOwnership_RejectsRequestWithoutIdentity(t *testing.T) {
	lookup := &stubOwner{owner: "11111111-1111-4111-8111-111111111111"}

	err := RequireOwnership(context.Background(), lookup, 1)
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("RequireOwnership = %v, want ErrNotAuthorized", err)
	}
	if lookup.calls != 0 {
		t.Error("a request with no identity should be rejected before hitting the database")
	}
}

func TestRequireOwnership_PropagatesLookupFailure(t *testing.T) {
	ctx := contextWithReviewer(t, "11111111-1111-4111-8111-111111111111")
	boom := errors.New("database is down")

	err := RequireOwnership(ctx, &stubOwner{err: boom}, 1)
	if !errors.Is(err, boom) {
		t.Fatalf("RequireOwnership = %v, want the lookup error", err)
	}
	if errors.Is(err, ErrNotAuthorized) {
		t.Error("an infrastructure failure must not be reported as an authorization decision")
	}
}
