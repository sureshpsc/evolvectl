package client

import grpc "google.golang.org/grpc"

func Field(cfg grpc.ServerConfig) string {
	return cfg.OldField
}
