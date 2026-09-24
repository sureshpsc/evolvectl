package client

import grpc "google.golang.org/grpc"

// Field reads the legacy config field.
func Field(cfg grpc.ServerConfig) string {
	return cfg.OldField
}
