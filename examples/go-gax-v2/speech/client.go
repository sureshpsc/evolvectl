// Package speech calls a backend through gax retries.
package speech

import (
	"context"

	gax "github.com/googleapis/gax-go"
)

// Recognize runs one call through gax.Invoke.
func Recognize(ctx context.Context, send func(context.Context) error, opts ...gax.CallOption) error {
	return gax.Invoke(ctx, func(ctx context.Context) error {
		return send(ctx)
	}, opts...)
}
