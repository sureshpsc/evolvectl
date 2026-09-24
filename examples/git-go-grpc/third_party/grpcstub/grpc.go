// Package grpc is a local stand-in with both the old and new APIs so the
// fixture can compile before and after the recipe rewrites call sites.
package grpc

import (
	"context"
	"errors"
)

// DialOption is a connection option.
type DialOption interface{ dialOpt() }

type dialOpt struct{}

func (dialOpt) dialOpt() {}

// WithTransportCredentials is accepted by both client constructors.
func WithTransportCredentials(creds any) DialOption { return dialOpt{} }

// ClientConn is a fake connection.
type ClientConn struct{ Target string }

// Close releases the connection.
func (c *ClientConn) Close() error { return nil }

// DialContext is the pre-upgrade constructor.
func DialContext(ctx context.Context, target string, opts ...DialOption) (*ClientConn, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	return &ClientConn{Target: target}, nil
}

// NewClient is the post-upgrade constructor.
func NewClient(target string, opts ...DialOption) (*ClientConn, error) {
	return &ClientConn{Target: target}, nil
}

// ServerConfig has both field names so either call site compiles.
type ServerConfig struct {
	OldField string
	NewField string
}
