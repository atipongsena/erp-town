package events

import "context"

type streamIDKey struct{}

// StreamID is the Redis entry currently being delivered to a Handler. It is
// transport metadata, separate from the envelope's business event ID.
func StreamID(ctx context.Context) string {
	id, _ := ctx.Value(streamIDKey{}).(string)
	return id
}

// WithStreamID attaches the stream position before delivering an envelope.
func WithStreamID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, streamIDKey{}, id)
}
