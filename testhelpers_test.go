package logus

import "context"

// fakeGinContext mimics the Get/Set surface of *gin.Context without importing gin.
type fakeGinContext struct {
	context.Context
	values map[string]any
}

func newFakeGinContext() *fakeGinContext {
	return &fakeGinContext{Context: context.Background(), values: map[string]any{}}
}

func (f *fakeGinContext) Get(key string) (any, bool) {
	v, ok := f.values[key]
	return v, ok
}

func (f *fakeGinContext) Set(key string, value any) {
	f.values[key] = value
}
