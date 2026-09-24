package client

import (
	"context"

	gax "github.com/googleapis/gax-go/v2"
)

func Get(ctx context.Context, opts ...gax.CallOption) error {
	return gax.Invoke(ctx, func(ctx context.Context) error {
		return nil
	}, opts...)
}
