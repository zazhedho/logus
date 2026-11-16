package logus

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestRequestIDFromContextReturnsStoredID(t *testing.T) {
	id := uuid.New()
	got, ok := RequestIDFromContext(WithRequestID(context.Background(), id))
	if !ok || got != id {
		t.Fatalf("got (%v, %v), want (%v, true)", got, ok, id)
	}
}

func TestRequestIDFromContextMissing(t *testing.T) {
	if got, ok := RequestIDFromContext(context.Background()); ok || got != uuid.Nil {
		t.Fatalf("background: got (%v, %v), want (Nil, false)", got, ok)
	}
	//nolint:staticcheck // nil context is deliberately tolerated
	if got, ok := RequestIDFromContext(nil); ok || got != uuid.Nil {
		t.Fatalf("nil: got (%v, %v), want (Nil, false)", got, ok)
	}
}

func TestWithRequestIDNilContext(t *testing.T) {
	id := uuid.New()
	//nolint:staticcheck // nil context is deliberately tolerated
	ctx := WithRequestID(nil, id)
	got, ok := RequestIDFromContext(ctx)
	if !ok || got != id {
		t.Fatalf("got (%v, %v), want (%v, true)", got, ok, id)
	}
}

func TestRequestIDPrefersTypedContextValue(t *testing.T) {
	a := uuid.New()
	ctx := WithRequestID(context.Background(), a)
	if got := RequestID(ctx); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDReadsUUIDFromGetter(t *testing.T) {
	a := uuid.New()
	f := newFakeGinContext()
	f.Set(CtxKeyID, a)
	if got := RequestID(f); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDParsesStringFromGetter(t *testing.T) {
	a := uuid.New()
	f := newFakeGinContext()
	f.Set(CtxKeyID, a.String())
	if got := RequestID(f); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDReadsPlainStringKey(t *testing.T) {
	a := uuid.New()
	//nolint:staticcheck // plain string key is the legacy behavior under test
	ctx := context.WithValue(context.Background(), CtxKeyID, a)
	if got := RequestID(ctx); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDGeneratesAndStoresInSetter(t *testing.T) {
	f := newFakeGinContext()
	got := RequestID(f)
	if got == uuid.Nil {
		t.Fatal("expected generated id")
	}
	if v, _ := f.Get(CtxKeyID); v != got {
		t.Fatalf("stored %v, want %v", v, got)
	}
	if again := RequestID(f); again != got {
		t.Fatalf("not stable: %v vs %v", again, got)
	}
}

func TestRequestIDReplacesInvalidStoredString(t *testing.T) {
	f := newFakeGinContext()
	f.Set(CtxKeyID, "not-a-uuid")
	got := RequestID(f)
	if got == uuid.Nil {
		t.Fatal("expected generated id")
	}
	if v, _ := f.Get(CtxKeyID); v != got {
		t.Fatalf("stored %v, want %v", v, got)
	}
}

func TestRequestIDPlainContextGeneratesEachCall(t *testing.T) {
	ctx := context.Background()
	a, b := RequestID(ctx), RequestID(ctx)
	if a == uuid.Nil || b == uuid.Nil || a == b {
		t.Fatalf("expected two distinct non-nil ids, got %v and %v", a, b)
	}
}

func TestRequestIDNilContext(t *testing.T) {
	//nolint:staticcheck // nil context is deliberately tolerated
	if got := RequestID(nil); got == uuid.Nil {
		t.Fatal("expected generated id")
	}
}
