package main

import (
	"encoding/json"
	"os"
)

type request struct {
	ProtocolVersion string `json:"protocolVersion"`
	Method          string `json:"method"`
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		os.Exit(2)
	}
	if req.Method != "handshake" {
		os.Exit(3)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"protocolVersion": "1.0",
		"adapter":         map[string]string{"name": "demo", "version": "0.1.0"},
		"capabilities":    []string{"discover.projects", "dependencies.inventory"},
	})
}
