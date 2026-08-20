package poolscheduler

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.CheckInterval != 2*time.Minute {
		t.Errorf("CheckInterval = %v, want 2m", cfg.CheckInterval)
	}
	if cfg.LeaseCheckInterval != time.Minute {
		t.Errorf("LeaseCheckInterval = %v, want 1m", cfg.LeaseCheckInterval)
	}
	if !cfg.RefreshCheckEnabled {
		t.Error("RefreshCheckEnabled should default to true")
	}
}

func TestNewSchedulerDefaultsConfig(t *testing.T) {
	// A nil config falls back to DefaultConfig.
	s := NewScheduler(nil, nil)
	if s.config == nil || s.config.CheckInterval != 2*time.Minute {
		t.Errorf("expected default config, got %+v", s.config)
	}
	if s.running {
		t.Error("scheduler should not be running before Start")
	}
}

func TestNewSchedulerCustomConfig(t *testing.T) {
	cfg := &Config{CheckInterval: 30 * time.Second, LeaseCheckInterval: 10 * time.Second}
	s := NewScheduler(cfg, nil)
	if s.config.CheckInterval != 30*time.Second {
		t.Errorf("CheckInterval = %v, want 30s", s.config.CheckInterval)
	}
}

func TestStopBeforeStartIsSafe(t *testing.T) {
	// Stop with a nil cancel func must not panic.
	s := NewScheduler(nil, nil)
	s.Stop()
	if s.running {
		t.Error("running should be false after Stop")
	}
}
