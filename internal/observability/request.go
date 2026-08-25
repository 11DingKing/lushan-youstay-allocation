package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type requestKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestKey{}, id)
}
func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestKey{}).(string)
	return value
}
func NewRequestID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "request-unknown"
	}
	return "req_" + hex.EncodeToString(buffer)
}
