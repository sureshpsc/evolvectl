// Package redact removes credential-shaped text from reports and logs.
package redact

import "regexp"

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(https?://)[^/\s:]+:[^/\s@]+@`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgho_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9\-._~+/]+=*\b`),
	regexp.MustCompile(`(?i)\b(token|password|secret|api[_-]?key)\s*[=:]\s*\S+`),
}

// Text redacts known secret shapes. It does not claim to find every secret.
func Text(s string) string {
	out := s
	for i, p := range patterns {
		switch i {
		case 0:
			out = p.ReplaceAllString(out, "${1}[redacted]@")
		case 6:
			out = p.ReplaceAllString(out, "${1} [redacted]")
		case 7:
			out = p.ReplaceAllString(out, "${1}=[redacted]")
		default:
			out = p.ReplaceAllString(out, "[redacted]")
		}
	}
	return out
}
