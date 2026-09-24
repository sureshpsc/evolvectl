// Package apperr carries an exit code and an actionable hint.
package apperr

import "fmt"

// Error is a user-facing failure with a stable key.
type Error struct {
	Code    int
	Key     string
	Message string
	Hint    string
}

func (e *Error) Error() string {
	if e.Key != "" {
		return e.Key + ": " + e.Message
	}
	return e.Message
}

// New builds an error.
func New(code int, key, message, hint string) *Error {
	return &Error{Code: code, Key: key, Message: message, Hint: hint}
}

// Format prints the error and its next step.
func Format(err error) string {
	var e *Error
	if ok := as(err, &e); ok {
		msg := e.Error()
		if e.Hint != "" {
			msg += "\nFix: " + e.Hint
		}
		return msg
	}
	return err.Error()
}

func as(err error, target **Error) bool {
	if err == nil {
		return false
	}
	ee, ok := err.(*Error)
	if ok {
		*target = ee
		return true
	}
	return false
}

// CodeOf returns the process status for err.
func CodeOf(err error) int {
	if err == nil {
		return 0
	}
	var e *Error
	if as(err, &e) {
		return e.Code
	}
	return 1
}

// Wrapf adds context while preserving an evolvectl exit code.
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	prefix := fmt.Sprintf(format, args...)
	var e *Error
	if as(err, &e) {
		cp := *e
		cp.Message = prefix + ": " + e.Message
		return &cp
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
