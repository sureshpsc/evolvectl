// Package idgen creates stable, URL-safe identifiers.
package idgen

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// New returns prefix plus 8 random hex bytes.
func New(prefix string) string {
	var b [8]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		sum := sha256.Sum256([]byte(prefix + err.Error()))
		return prefix + hex.EncodeToString(sum[:8])
	}
	return prefix + hex.EncodeToString(b[:])
}

// Hash returns a short sha256 hex digest.
func Hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = io.WriteString(h, p)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Short is the first n hex chars of Hash, n clamped to 64.
func Short(n int, parts ...string) string {
	s := Hash(parts...)
	if n > len(s) {
		n = len(s)
	}
	if n < 1 {
		return s
	}
	return s[:n]
}

// DepID is a stable dependency id.
func DepID(ecosystem, name, manifest string) string {
	return fmt.Sprintf("dep:%s:%s:%s", ecosystem, name, manifest)
}

// ProjectID is a stable project id from a relative path.
func ProjectID(rel string) string {
	if rel == "" || rel == "." {
		return "prj:."
	}
	return "prj:" + rel
}
