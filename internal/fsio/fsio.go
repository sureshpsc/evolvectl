// Package fsio holds small filesystem helpers.
package fsio

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HashFile returns the sha256 hex of a file.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashBytes returns the sha256 hex of b.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// WriteAtomic replaces path with data.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".evolvectl-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(path)
		if err2 := os.Rename(tmpName, path); err2 != nil {
			return err
		}
	}
	cleanup = false
	return nil
}

// WithinRoot reports whether path is inside root after cleaning.
func WithinRoot(root, path string) (string, bool) {
	root = filepath.Clean(root)
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, path)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

// RelSlash returns a slash-separated path relative to root.
func RelSlash(root, path string) string {
	abs, ok := WithinRoot(root, path)
	if !ok {
		return "[outside-root]"
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "[outside-root]"
	}
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}
