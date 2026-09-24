// Package sdk documents the executable adapter handshake.
package sdk

// HandshakeRequest is sent to evolvectl-adapter-* on stdin.
type HandshakeRequest struct {
	ProtocolVersion string `json:"protocolVersion"`
	Method          string `json:"method"`
	Client          struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"client"`
}

// HandshakeResponse is the adapter's capability advertisement.
type HandshakeResponse struct {
	ProtocolVersion string   `json:"protocolVersion"`
	Adapter         Identity `json:"adapter"`
	Capabilities    []string `json:"capabilities"`
}

// Identity names an adapter build.
type Identity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
