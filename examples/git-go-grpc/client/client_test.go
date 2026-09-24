package client

import (
	"context"
	"testing"

	"google.golang.org/grpc"
)

func TestConnect(t *testing.T) {
	conn, err := Connect(context.Background(), "localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	if conn.Target != "localhost:1" {
		t.Fatalf("target %s", conn.Target)
	}
}

func TestField(t *testing.T) {
	got := Field(grpc.ServerConfig{OldField: "x"})
	if got != "x" {
		t.Fatalf("got %s", got)
	}
}
