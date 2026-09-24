package client

import (
	"context"

	gax "github.com/googleapis/gax-go/v2"
)

func Get(ctx context.Context, opts ...gax.CallOption) error {
	return gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		return nil
	}, opts...)
}
