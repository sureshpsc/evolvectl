package client

import (
	"context"

	"github.com/googleapis/gax-go/v2"
)

var call = gax.APICall(func(ctx context.Context) error { return nil })
