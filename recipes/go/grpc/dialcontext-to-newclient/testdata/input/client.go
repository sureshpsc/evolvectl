package client

import (
	"context"

	"google.golang.org/grpc"
)

func Connect(ctx context.Context, endpoint string) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, endpoint, grpc.WithTransportCredentials(nil))
}
