package speech

import (
	"context"
	"testing"
)

func TestRecognizeCallsSend(t *testing.T) {
	called := false
	err := Recognize(context.Background(), func(context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}
