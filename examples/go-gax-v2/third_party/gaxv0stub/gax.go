// Package gax is an offline stub with the exported shape of gax-go at da06d194a00e (2016).
package gax

import "context"

// APICall takes only a context in this version.
type APICall func(context.Context) error

// CallOption configures a call.
type CallOption interface{ Resolve(*CallSettings) }

// CallSettings holds per-call settings.
type CallSettings struct {
	Retry func() int
}

// Invoke calls call once.
func Invoke(ctx context.Context, call APICall, opts ...CallOption) error {
	var s CallSettings
	for _, o := range opts {
		o.Resolve(&s)
	}
	return call(ctx)
}

// PathTemplate was removed in v2.
type PathTemplate struct{ raw string }

// MustCompilePathTemplate was removed in v2.
func MustCompilePathTemplate(t string) *PathTemplate { return &PathTemplate{raw: t} }
