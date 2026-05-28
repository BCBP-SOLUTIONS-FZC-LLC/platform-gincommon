package middleware

import (
	"context"

	"google.golang.org/grpc"
)

// wrappedStream wraps grpc.ServerStream with a replaced context.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (ws *wrappedStream) Context() context.Context {
	return ws.ctx
}
