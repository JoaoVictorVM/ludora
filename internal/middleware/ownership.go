package middleware

import (
	"context"
	"errors"
)

// ErrNotAuthorized is returned when the requester's anonymous identity does not
// match the resource being mutated. Callers map it to a single generic message:
// distinguishing "not yours" from "does not exist" would let anyone probe which
// review ids are real.
var ErrNotAuthorized = errors.New("middleware: not authorized")

// ResourceOwner reports who owns the resource with the given id, returning an
// empty owner when it does not exist — a missing resource and someone else's
// resource must be indistinguishable to the requester.
type ResourceOwner interface {
	OwnerOf(ctx context.Context, id int64) (string, error)
}

// RequireOwnership confirms the request's anonymous identity owns the resource.
// Every mutating endpoint routes through here so the failure behaviour — generic
// message, no existence leak — is identical everywhere.
func RequireOwnership(ctx context.Context, owner ResourceOwner, id int64) error {
	reviewerUUID, ok := ReviewerID(ctx)
	if !ok {
		return ErrNotAuthorized
	}

	ownerUUID, err := owner.OwnerOf(ctx, id)
	if err != nil {
		// A lookup failure is an infrastructure problem, not an authorization
		// decision, so it is propagated instead of masked as "not yours".
		return err
	}

	if ownerUUID == "" || ownerUUID != reviewerUUID {
		return ErrNotAuthorized
	}

	return nil
}
