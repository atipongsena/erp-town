package platform

import "context"

type requestIDKey struct{}

// WithRequestID attaches a server-generated correlation id to an operation.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the correlation id, or empty for work outside an HTTP request.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
