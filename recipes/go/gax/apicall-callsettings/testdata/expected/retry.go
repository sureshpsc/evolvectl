package client

import (
	"context"

	"github.com/googleapis/gax-go/v2"
)

var call = gax.APICall(func(ctx context.Context, _ gax.CallSettings) error { return nil })
