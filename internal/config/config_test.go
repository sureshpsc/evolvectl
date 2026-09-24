package config

import "testing"

func TestDefaultsValidate(t *testing.T) {
	if err := Validate(Default()); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.Version = 2
	if err := Validate(cfg); err == nil {
		t.Fatal("expected version error")
	}
}
