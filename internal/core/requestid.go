package core

import (
	"context"

	"github.com/google/uuid"
)

// Gin-style context keys. The public logus package mirrors these values as
// CtxKeyID and CtxKeyUserID.
const (
	CtxKeyID     = "CTX_ID"
	CtxKeyUserID = "userId"
)

type requestIDKey struct{}

// Get/Set method shapes of gin-style contexts. Older gin versions use string
// keys; gin v1.11 and later use any-typed keys.
type (
	stringKeyGetter interface{ Get(key string) (any, bool) }
	anyKeyGetter    interface{ Get(key any) (any, bool) }
	stringKeySetter interface{ Set(key string, value any) }
	anyKeySetter    interface{ Set(key any, value any) }
)

// ctxGet reads key through a Get method of either signature family.
func ctxGet(ctx context.Context, key string) (any, bool) {
	switch g := ctx.(type) {
	case stringKeyGetter:
		return g.Get(key)
	case anyKeyGetter:
		return g.Get(key)
	}
	return nil, false
}

// ctxSet stores value under key through a Set method of either signature
// family. It reports whether ctx had such a method.
func ctxSet(ctx context.Context, key string, value any) bool {
	switch s := ctx.(type) {
	case stringKeySetter:
		s.Set(key, value)
		return true
	case anyKeySetter:
		s.Set(key, value)
		return true
	}
	return false
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
// value set by WithRequestID, a Get method keyed by CtxKeyID, and a plain
// string context key CtxKeyID. Get may have the signature Get(string) (any,
// bool) or Get(any) (any, bool), the latter being *gin.Context in gin v1.11
// and later. Values may be a uuid.UUID or a parsable string. When nothing
// valid is found a new id is generated and, if ctx has a Set(string, any) or
// Set(any, any) method, stored under CtxKeyID.
func RequestID(ctx context.Context) uuid.UUID {
	if ctx == nil {
		return newRequestID()
	}
	if id, ok := RequestIDFromContext(ctx); ok {
		return id
	}
	if v, found := ctxGet(ctx, CtxKeyID); found {
		if id, ok := toUUID(v); ok {
			return id
		}
	}
	//nolint:staticcheck // plain string key kept for backward compatibility
	if id, ok := toUUID(ctx.Value(CtxKeyID)); ok {
		return id
	}

	id := newRequestID()
	ctxSet(ctx, CtxKeyID, id)
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
