package config

import (
	"testing"
	"time"
)

func TestScaleSettings(t *testing.T) {
	cfg := Default()
	if got := cfg.Validation.TestTimeoutDuration(); got != DefaultTestTimeout {
		t.Fatalf("default timeout %s", got)
	}
	cfg.Validation.TestTimeout = "25m"
	if got := cfg.Validation.TestTimeoutDuration(); got != 25*time.Minute {
		t.Fatalf("timeout %s", got)
	}
	for _, bad := range []string{"soon", "-1m", "0s"} {
		cfg.Validation.TestTimeout = bad
		if Validate(cfg) == nil {
			t.Fatalf("test_timeout %q should be rejected", bad)
		}
	}
	cfg = Default()
	cfg.Execution.DryRunCopy = "rsync"
	if Validate(cfg) == nil {
		t.Fatal("unknown dry_run_copy should be rejected")
	}
	if (Validation{}).TestTimeoutDuration() != DefaultTestTimeout {
		t.Fatal("empty timeout should use the default")
	}
}

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
