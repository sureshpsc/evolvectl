// Package gax is an offline stub with the exported shape of gax-go/v2 at v2.0.2.
package gax

import "context"

// APICall receives the resolved settings in v2.
type APICall func(context.Context, CallSettings) error

// CallOption configures a call.
type CallOption interface{ Resolve(*CallSettings) }

// CallSettings holds per-call settings.
type CallSettings struct {
	Retry func() int
	GRPC  []string
}

// Invoke calls call once with the resolved settings.
func Invoke(ctx context.Context, call APICall, opts ...CallOption) error {
	var s CallSettings
	for _, o := range opts {
		o.Resolve(&s)
	}
	return call(ctx, s)
}

// XGoogHeader was added in v2.
func XGoogHeader(keyval ...string) string { return "" }
