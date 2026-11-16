package logus

import (
	"context"

	"github.com/google/uuid"
)

const (
	// CtxKeyID is the gin-style context key under which the request id is stored.
	CtxKeyID = "CTX_ID"
	// CtxKeyUserID is the gin-style context key under which the user id is stored.
	CtxKeyUserID = "userId"
)

type requestIDKey struct{}

type valueGetter interface {
	Get(key string) (any, bool)
}

type valueSetter interface {
	Set(key string, value any)
}

// WithRequestID returns a copy of ctx carrying id. A nil ctx is treated as
// context.Background().
func WithRequestID(ctx context.Context, id uuid.UUID) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext returns the request id stored by WithRequestID.
func RequestIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	if ctx == nil {
		return uuid.Nil, false
	}
	id, ok := ctx.Value(requestIDKey{}).(uuid.UUID)
	if !ok || id == uuid.Nil {
		return uuid.Nil, false
	}
	return id, true
}

// RequestID resolves the request id for ctx. It looks, in order, at the typed
// value set by WithRequestID, a Get(string) (any, bool) method keyed by
// CtxKeyID, and a plain string context key CtxKeyID. Values may be a uuid.UUID
// or a parsable string. When nothing valid is found a new id is generated and,
// if ctx has a Set(string, any) method, stored under CtxKeyID.
func RequestID(ctx context.Context) uuid.UUID {
	if ctx == nil {
		return newRequestID()
	}
	if id, ok := RequestIDFromContext(ctx); ok {
		return id
	}
	if g, ok := ctx.(valueGetter); ok {
		if v, found := g.Get(CtxKeyID); found {
			if id, ok := toUUID(v); ok {
				return id
			}
		}
	}
	//nolint:staticcheck // plain string key kept for backward compatibility
	if id, ok := toUUID(ctx.Value(CtxKeyID)); ok {
		return id
	}

	id := newRequestID()
	if s, ok := ctx.(valueSetter); ok {
		s.Set(CtxKeyID, id)
	}
	return id
}

func toUUID(v any) (uuid.UUID, bool) {
	switch t := v.(type) {
	case uuid.UUID:
		return t, t != uuid.Nil
	case string:
		id, err := uuid.Parse(t)
		return id, err == nil && id != uuid.Nil
	}
	return uuid.Nil, false
}

func newRequestID() uuid.UUID {
	if id, err := uuid.NewV7(); err == nil {
		return id
	}
	return uuid.New()
}
